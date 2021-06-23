package nodeagent

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/mntr"
)

type FirewallEnsurer interface {
	Query(desired common.Firewall) (current common.FirewallCurrent, ensure func() error, err error)
}

type FirewallEnsurerFunc func(desired common.Firewall) (current common.FirewallCurrent, ensure func() error, err error)

func (f FirewallEnsurerFunc) Query(desired common.Firewall) (current common.FirewallCurrent, ensure func() error, err error) {
	return f(desired)
}

type NetworkingEnsurer interface {
	Query(desired common.Networking) (current common.NetworkingCurrent, ensure func() error, err error)
}

type NetworkingEnsurerFunc func(desired common.Networking) (current common.NetworkingCurrent, ensure func() error, err error)

func (f NetworkingEnsurerFunc) Query(desired common.Networking) (current common.NetworkingCurrent, ensure func() error, err error) {
	return f(desired)
}

type Dependency struct {
	Installer Installer
	Desired   common.Package
	Current   common.Package
}

type Converter interface {
	ToDependencies(common.Software) []*Dependency
	ToSoftware([]*Dependency, func(Dependency) common.Package) common.Software
}

type Installer interface {
	Current() (common.Package, error)
	Ensure(uninstall, install common.Package, leaveOSRepositories bool) error
	Equals(other Installer) bool
	Is(other Installer) bool
	fmt.Stringer
}

func prepareQuery(
	monitor mntr.Monitor,
	commit string,
	firewallEnsurer FirewallEnsurer,
	networkingEnsurer NetworkingEnsurer,
	conv Converter,
) func(common.NodeAgentSpec, *common.NodeAgentCurrent) (func() error, error) {

	if err := os.MkdirAll("/var/orbiter", 0700); err != nil {
		panic(err)
	}

	return func(desired common.NodeAgentSpec, curr *common.NodeAgentCurrent) (func() error, error) {
		curr.Commit = commit

		curr.NodeIsReady = isReady()

		defer persistReadyness(curr.NodeIsReady)

		dateTime, err := exec.Command("uptime", "-s").CombinedOutput()
		if err != nil {
			return noop, err
		}

		//dateTime := strings.Fields(string(who))[2:]
		//str := strings.Join(dateTime, " ") + ":00"
		t, err := time.Parse("2006-01-02 15:04:05", strings.TrimSuffix(string(dateTime), "\n"))
		if err != nil {
			return noop, err
		}

		curr.Booted = t

		if desired.RebootRequired.After(curr.Booted) {
			curr.NodeIsReady = false
			if !desired.ChangesAllowed {
				monitor.Info("Not rebooting as changes are not allowed")
				return noop, nil
			}
			return func() error {
				monitor.Info("Rebooting")
				if err := exec.Command("sudo", "reboot").Run(); err != nil {
					return fmt.Errorf("rebooting system failed: %w", err)
				}
				return nil
			}, nil
		}

		var ensureNetworking func() error
		curr.Networking, ensureNetworking, err = networkingEnsurer.Query(*desired.Networking)
		if err != nil {
			return noop, err
		}
		curr.Networking.Sort()

		var ensureFirewall func() error
		curr.Open, ensureFirewall, err = firewallEnsurer.Query(*desired.Firewall)
		if err != nil {
			return noop, err
		}
		curr.Open.Sort()

		installedSw, err := deriveTraverse(queryFunc(monitor), conv.ToDependencies(*desired.Software))
		if err != nil {
			return noop, err
		}

		curr.Software = conv.ToSoftware(installedSw, func(dep Dependency) common.Package {
			return dep.Current
		})

		divergentSw := deriveFilter(divergent, append([]*Dependency(nil), installedSw...))
		if len(divergentSw) == 0 && ensureFirewall == nil && ensureNetworking == nil {
			curr.NodeIsReady = true
			return noop, nil
		}

		if curr.NodeIsReady {
			curr.NodeIsReady = false
			monitor.Changed("Marked node as unready")
			return noop, nil
		}

		return func() error {

			if !desired.ChangesAllowed {
				monitor.Info("Not ensuring anything as changes are not allowed")
				return nil
			}

			if ensureNetworking != nil {
				if err := ensureNetworking(); err != nil {
					return err
				}
				curr.Networking = desired.Networking.ToCurrent()
				monitor.Changed("networking changed")
				curr.Networking.Sort()
			}

			if ensureFirewall != nil {
				if err := ensureFirewall(); err != nil {
					return err
				}
				curr.Open = desired.Firewall.ToCurrent()
				monitor.Changed("firewall changed")
				curr.Open.Sort()
			}

			if len(divergentSw) > 0 {
				monitor.WithField("software", deriveFmap(func(dependency *Dependency) string {
					return dependency.Installer.String()
				}, divergentSw)).Info("Ensuring software")
			}
			ensureDep := ensureFunc(monitor, conv, curr, desired.LeaveOSRepositories)
			_, err := deriveTraverse(ensureDep, divergentSw)
			return err
		}, nil
	}
}

func queryFunc(monitor mntr.Monitor) func(dep *Dependency) (*Dependency, error) {
	return func(dep *Dependency) (*Dependency, error) {
		version, err := dep.Installer.Current()
		if err != nil {
			return dep, err
		}
		dep.Current = version
		monitor.Debug("Dependency found")
		return dep, nil
	}
}

func divergent(dep *Dependency) bool {
	return !dep.Desired.Equals(common.Package{}) && !dep.Desired.Equals(dep.Current)
}

func ensureFunc(monitor mntr.Monitor, conv Converter, curr *common.NodeAgentCurrent, leaveOSRepositories bool) func(dep *Dependency) (*Dependency, error) {
	return func(dep *Dependency) (*Dependency, error) {
		monitor.WithFields(map[string]interface{}{
			"dependency": dep.Installer,
			"from":       dep.Current.Version,
			"to":         dep.Desired.Version,
		}).Info("Ensuring dependency")

		if err := dep.Installer.Ensure(dep.Current, dep.Desired, leaveOSRepositories); err != nil {
			return dep, err
		}

		curr.Software.Merge(conv.ToSoftware([]*Dependency{dep}, func(dep Dependency) common.Package {
			return dep.Desired
		}))
		monitor.WithFields(map[string]interface{}{
			"dependency": dep.Installer,
			"from":       dep.Current.Version,
			"to":         dep.Desired.Version,
		}).Changed("Dependency ensured")
		dep.Current = dep.Desired
		return dep, nil
	}
}

func merge(inferior []*Dependency, prior []*Dependency) []*Dependency {
	keep := deriveFilter(func(item *Dependency) bool {
		return !contains(prior, item)
	}, append([]*Dependency(nil), inferior...))
	return append(keep, prior...)
}

func contains(deps []*Dependency, dep *Dependency) bool {
	return deriveAny(func(item *Dependency) bool {
		return is(item, dep)
	}, deps)
}

func is(this *Dependency, that *Dependency) bool {
	return this.Installer.Is(that.Installer)
}

func persistReadyness(ready bool) {
	if ready {
		if err := ioutil.WriteFile("/var/orbiter/ready", nil, 600); err != nil {
			panic(err)
		}
		return
	}
	if err := os.RemoveAll("/var/orbiter/ready"); err != nil && !os.IsNotExist(err) {
		panic(err)
	}
}

func isReady() bool {
	_, err := os.Stat("/var/orbiter/ready")
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	return err == nil
}

func noop() error { return nil }
