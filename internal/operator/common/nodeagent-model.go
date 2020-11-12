//go:generate goderive -autoname -dedup .

package common

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NodeAgentSpec struct {
	ChangesAllowed bool
	//	RebootEnabled  bool
	Software       *Software
	Firewall       *Firewall
	RebootRequired time.Time
}

type NodeAgentCurrent struct {
	NodeIsReady bool `mapstructure:"ready" yaml:"ready"`
	Software    Software
	Open        []*Allowed
	Commit      string
	Booted      time.Time
}

type Software struct {
	Swap             Package `yaml:",omitempty"`
	Kubelet          Package `yaml:",omitempty"`
	Kubeadm          Package `yaml:",omitempty"`
	Kubectl          Package `yaml:",omitempty"`
	Containerruntime Package `yaml:",omitempty"`
	KeepaliveD       Package `yaml:",omitempty"`
	Nginx            Package `yaml:",omitempty"`
	SSHD             Package `yaml:",omitempty"`
	Hostname         Package `yaml:",omitempty"`
	Sysctl           Package `yaml:",omitempty"`
	Health           Package `yaml:",omitempty"`
}

func (s *Software) Merge(sw Software) {

	zeroPkg := Package{}

	if !sw.Containerruntime.Equals(zeroPkg) {
		s.Containerruntime = sw.Containerruntime
	}

	if !sw.KeepaliveD.Equals(zeroPkg) {
		s.KeepaliveD = sw.KeepaliveD
	}

	if !sw.Nginx.Equals(zeroPkg) {
		s.Nginx = sw.Nginx
	}

	if !sw.Kubeadm.Equals(zeroPkg) {
		s.Kubeadm = sw.Kubeadm
	}

	if !sw.Kubelet.Equals(zeroPkg) {
		s.Kubelet = sw.Kubelet
	}

	if !sw.Kubectl.Equals(zeroPkg) {
		s.Kubectl = sw.Kubectl
	}

	if !sw.Swap.Equals(zeroPkg) {
		s.Swap = sw.Swap
	}

	if !sw.SSHD.Equals(zeroPkg) {
		s.SSHD = sw.SSHD
	}

	if !sw.Hostname.Equals(zeroPkg) {
		s.Hostname = sw.Hostname
	}

	if !sw.Sysctl.Equals(zeroPkg) && s.Sysctl.Config == nil {
		s.Sysctl.Config = make(map[string]string)
	}
	for key, value := range sw.Sysctl.Config {
		s.Sysctl.Config[key] = value
	}
	if !sw.Health.Equals(zeroPkg) && s.Health.Config == nil {
		s.Health.Config = make(map[string]string)
	}
	for key, value := range sw.Health.Config {
		s.Health.Config[key] = value
	}
}

type Package struct {
	Version string            `yaml:",omitempty"`
	Config  map[string]string `yaml:",omitempty"`
}

var prune = regexp.MustCompile("[^a-zA-Z0-9]+")

func configEquals(this, that map[string]string) bool {
	if this == nil || that == nil {
		return this == nil && that == nil
	}
	if len(this) != len(that) {
		return false
	}
	for k, v := range this {
		thatv, ok := that[k]
		if !ok {
			return false
		}

		if prune.ReplaceAllString(v, "") != prune.ReplaceAllString(thatv, "") {
			return false
		}
	}
	return true
}

func (p Package) Equals(other Package) bool {
	return PackageEquals(p, other)
}
func PackageEquals(this, that Package) bool {
	equals := this.Version == that.Version &&
		configEquals(this.Config, that.Config)
	return equals
}

type Firewall struct {
	mux sync.Mutex          `yaml:"-"`
	FW  map[string]*Allowed `yaml:",inline"`
}

func ToFirewall(fw map[string]*Allowed) Firewall {
	return Firewall{FW: fw}
}

func (f *Firewall) Merge(fw Firewall) {
	f.mux.Lock()
	defer f.mux.Unlock()
	if len(fw.FW) > 0 && f.FW == nil {
		f.FW = make(map[string]*Allowed)
	}
	for key, value := range fw.FW {
		f.FW[key] = value
	}
}

func (f *Firewall) Ports() Ports {
	ports := make([]*Allowed, 0)
	for _, value := range f.FW {
		ports = append(ports, value)
	}
	return ports
}

type Ports []*Allowed

func (p Ports) String() string {
	strs := make([]string, len(p))
	for idx, port := range p {
		strs[idx] = fmt.Sprintf("%s/%s", port.Port, port.Protocol)
	}
	return strings.Join(strs, " ")
}

type Allowed struct {
	Port     string
	Protocol string
}

func (a Allowed) Validate() error {
	_, err := strconv.ParseInt(a.Port, 10, 16)
	if err != nil {
		return err
	}
	if a.Protocol != "udp" && a.Protocol != "tcp" {
		return fmt.Errorf("supported protocols are udp/tcp, got: %s", a.Protocol)
	}
	return nil
}

func (f Firewall) Contains(other Firewall) bool {
	for name, port := range other.FW {
		found, ok := f.FW[name]
		if !ok {
			return false
		}
		if !deriveEqualPort(*port, *found) {
			return false
		}
	}
	return true
}

func (f Firewall) IsContainedIn(ports []*Allowed) bool {
checks:
	for _, fwPort := range f.FW {
		for _, port := range ports {
			if deriveEqualPort(*port, *fwPort) {
				continue checks
			}
		}
		return false
	}
	return true
}

type NodeAgentsCurrentKind struct {
	Kind    string
	Version string
	Current CurrentNodeAgents
}

type CurrentNodeAgents struct {
	// NA is exported for yaml (de)serialization and not intended to be accessed by any other code outside this package
	NA  map[string]*NodeAgentCurrent `yaml:",inline"`
	mux sync.Mutex                   `yaml:"-"`
}

func (n *CurrentNodeAgents) Set(id string, na *NodeAgentCurrent) {
	n.mux.Lock()
	defer n.mux.Unlock()
	if n.NA == nil {
		n.NA = make(map[string]*NodeAgentCurrent)
	}
	n.NA[id] = na
}

func (n *CurrentNodeAgents) Get(id string) (*NodeAgentCurrent, bool) {
	n.mux.Lock()
	defer n.mux.Unlock()

	if n.NA == nil {
		n.NA = make(map[string]*NodeAgentCurrent)
	}

	na, ok := n.NA[id]
	if !ok {
		na = &NodeAgentCurrent{
			Open: make([]*Allowed, 0),
		}
		n.NA[id] = na
	}
	return na, ok

}

type NodeAgentsSpec struct {
	Commit     string
	NodeAgents DesiredNodeAgents
}

type DesiredNodeAgents struct {
	// NA is exported for yaml (de)serialization and not intended to be accessed by any other code outside this package
	NA  map[string]*NodeAgentSpec `yaml:",inline"`
	mux sync.Mutex                `yaml:"-"`
}

func (n *DesiredNodeAgents) Delete(id string) {
	n.mux.Lock()
	defer n.mux.Unlock()
	delete(n.NA, id)
}

func (n *DesiredNodeAgents) Get(id string) (*NodeAgentSpec, bool) {
	n.mux.Lock()
	defer n.mux.Unlock()

	if n.NA == nil {
		n.NA = make(map[string]*NodeAgentSpec)
	}

	na, ok := n.NA[id]
	if !ok {
		na = &NodeAgentSpec{
			Software: &Software{},
			Firewall: &Firewall{
				FW: make(map[string]*Allowed),
			},
		}
		n.NA[id] = na
	}
	return na, ok
}

type NodeAgentsDesiredKind struct {
	Kind    string
	Version string
	Spec    NodeAgentsSpec `yaml:",omitempty"`
}
