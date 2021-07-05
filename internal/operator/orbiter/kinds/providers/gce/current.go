package gce

import (
	"bytes"

	"github.com/caos/orbos/internal/executables"

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

func (c *Current) Kubernetes() infra.Kubernetes {
	return infra.Kubernetes{
		Apply: bytes.NewReader(executables.PreBuilt("kubernetes_gce.yaml")),
		/*		CloudController: infra.CloudControllerManager{
					Supported: true,
					CloudConfig: func(machine infra.Machine) io.Reader {
						instance := machine.(*instance)
						ctx := instance.context
						return strings.NewReader(fmt.Sprintf(`[Global]
				project-id = "%s"
				network-name = "%s"
				node-instance-prefix = "orbos-"
				multizone = true
				regional = true
				container-api-endpoint = "Don't use container API'"
				`,
							ctx.projectID,
							ctx.networkName,
							//instance.Zone(),
						))
					},
					ProviderName: "external",
				},*/
	}
}

func initPools(current *Current, desired *Spec, svc *machinesService, normalized []*normalizedLoadbalancer, machines core.MachinesService) error {

	current.Current.pools = make(map[string]infra.Pool)
	for pool := range desired.Pools {
		current.Current.pools[pool] = newInfraPool(pool, svc, normalized, machines)
	}

	pools, err := machines.ListPools()
	if err != nil {
		return nil
	}
	for _, pool := range pools {
		// Also return pools that are not configured
		if _, ok := current.Current.pools[pool]; !ok {
			current.Current.pools[pool] = newInfraPool(pool, svc, normalized, machines)
		}
	}
	return nil
}
