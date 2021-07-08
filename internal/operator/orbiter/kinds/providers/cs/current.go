package cs

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
	"github.com/caos/orbos/pkg/tree"
)

var _ infra.ProviderCurrent = (*Current)(nil)

type Current struct {
	Common  *tree.Common `yaml:",inline"`
	Current struct {
		pools      map[string]infra.Pool `yaml:"-"`
		Ingresses  map[string]*infra.Address
		cleanupped <-chan error `yaml:"-"`
	}
}

func (c *Current) Pools() map[string]infra.Pool {
	return c.Current.pools
}
func (c *Current) Ingresses() map[string]*infra.Address {
	return c.Current.Ingresses
}
func (c *Current) Cleanupped() <-chan error {
	return c.Current.cleanupped
}
func (c *Current) PrivateInterface() string { return "eth0" }

func (c *Current) Kubernetes() infra.Kubernetes {
	return infra.Kubernetes{}
}

func addPools(current *Current, spec *Spec, machinesSvc core.MachinesService) error {
	current.Current.pools = make(map[string]infra.Pool)
	for pool := range spec.Pools {
		current.Current.pools[pool] = newInfraPool(pool, machinesSvc)
	}

	unconfiguredPools, err := machinesSvc.ListPools()
	if err != nil {
		return nil
	}
	for idx := range unconfiguredPools {
		unconfiguredPool := unconfiguredPools[idx]
		if _, ok := current.Current.pools[unconfiguredPool]; !ok {
			current.Current.pools[unconfiguredPool] = newInfraPool(unconfiguredPool, machinesSvc)
		}
	}
	return nil
}
