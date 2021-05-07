package infra

import (
	"fmt"
	"io"
	"sort"

	"github.com/caos/orbos/pkg/kubernetes"
)

type Address struct {
	Location     string
	FrontendPort uint16
	BackendPort  uint16
}

func (a Address) String() string {
	return fmt.Sprintf("%s:%d", a.Location, a.FrontendPort)
}

type ProviderCurrent interface {
	Pools() map[string]Pool
	Ingresses() map[string]*Address
	Kubernetes() Kubernetes
}

type Kubernetes struct {
	CleanupAndApply func(k8sClient kubernetes.ClientInt) (io.Reader, error)
	CloudController CloudControllerManager
}

type CloudControllerManager struct {
	Supported    bool
	CloudConfig  func(machine Machine) io.Reader
	ProviderName string
}

type Ingress struct {
	Pools            []string
	HealthChecksPath string
}

type Pool interface {
	EnsureMembers() error
	EnsureMember(Machine) error
	GetMachines() (Machines, error)
	AddMachine() (Machine, error)
}

type Machine interface {
	ID() string
	IP() string
	Remove() error
	Execute(stdin io.Reader, cmd string) ([]byte, error)
	Shell() error
	WriteFile(path string, data io.Reader, permissions uint16) error
	ReadFile(path string, data io.Writer) error
	RebootRequired() (required bool, require func(), unrequire func())
	ReplacementRequired() (required bool, require func(), unrequire func())
}

type Machines []Machine

func (c Machines) ToChan() <-chan Machine {
	compChan := make(chan Machine)
	go func() {
		for _, comp := range c {
			compChan <- comp
		}
		close(compChan)
	}()
	return compChan
}

func (c Machines) String() string {
	list := ""
	for _, comp := range c {
		list += "|" + comp.ID()
	}
	if len(list) > 0 {
		list = list[1:]
	}
	return "(" + list + ")"
}

func (c Machines) Len() int           { return len(c) }
func (c Machines) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c Machines) Less(i, j int) bool { return c[i].ID() < c[j].ID() }

func (c Machines) IDs() []string {
	l := len(c)
	machines := make([]Machine, l, l)
	copy(machines, c)
	sort.Sort(Machines(machines))
	ids := make([]string, l, l)
	for idx, machine := range machines {
		ids[idx] = machine.ID()
	}
	return ids
}
