//go:generate goderive -autoname -dedup .

package common

import (
	"regexp"
	"sort"
	"sync"
	"time"
)

type NodeAgentSpec struct {
	ChangesAllowed      bool
	LeaveOSRepositories bool
	//	RebootEnabled  bool
	Software       *Software
	Networking     *Networking
	Firewall       *Firewall
	RebootRequired time.Time
}

type NodeAgentCurrent struct {
	NodeIsReady bool `mapstructure:"ready" yaml:"ready"`
	Software    Software
	Open        FirewallCurrent
	Networking  NetworkingCurrent
	Commit      string
	Booted      time.Time
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

type MarshallableSlice []string

func (m MarshallableSlice) MarshalYAML() (interface{}, error) {
	sort.Strings(m)
	type s []string
	return s(m), nil
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

	if _, ok := n.NA[id]; ok {
		n.NA[id] = nil
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
			Open:       make(FirewallCurrent, 0),
			Networking: make(NetworkingCurrent, 0),
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

	if _, ok := n.NA[id]; ok {
		n.NA[id] = nil
	}
	delete(n.NA, id)
}

func (n *DesiredNodeAgents) List() []string {
	n.mux.Lock()
	defer n.mux.Unlock()
	var ids []string
	for id := range n.NA {
		ids = append(ids, id)
	}
	return ids
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
			Software: &Software{
				Sysctl: Package{
					Config: map[string]string{
						string(IpForward):             "0",
						string(NonLocalBind):          "0",
						string(BridgeNfCallIptables):  "0",
						string(BridgeNfCallIp6tables): "0",
					},
				},
			},
			Firewall: &Firewall{
				Zones: map[string]*Zone{
					"internal": {
						Interfaces: []string{},
						FW:         map[string]*Allowed{},
						Services:   map[string]*Service{},
					}, "external": {
						Interfaces: []string{},
						FW:         map[string]*Allowed{},
						Services:   map[string]*Service{},
					},
				},
			},
			Networking: &Networking{
				Interfaces: map[string]*NetworkingInterface{},
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

type KernelModule string

const (
	IpForward             KernelModule = "net.ipv4.ip_forward"
	NonLocalBind          KernelModule = "net.ipv4.ip_nonlocal_bind"
	BridgeNfCallIptables  KernelModule = "net.bridge.bridge-nf-call-iptables"
	BridgeNfCallIp6tables KernelModule = "net.bridge.bridge-nf-call-ip6tables"
)
