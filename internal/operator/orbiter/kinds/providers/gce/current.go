package gce

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/caos/orbos/pkg/kubernetes"
	macherrs "k8s.io/apimachinery/pkg/api/errors"

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
		CleanupAndApply: func(k8sClient kubernetes.ClientInt) (io.Reader, error) {
			var create bool
			deployment, err := k8sClient.GetDeployment("gce-pd-csi-driver", "csi-gce-pd-controller")
			if macherrs.IsNotFound(err) {
				create = true
				err = nil
			}
			if err != nil {
				return nil, err
			}

			apply := func() io.Reader {
				return bytes.NewReader(executables.PreBuilt("kubernetes_gce.yaml"))
			}

			if create {
				return apply(), nil
			}

			containers := deployment.Spec.Template.Spec.Containers
			if len(containers) != 5 {
				return apply(), nil
			}
			for idx := range containers {
				container := containers[idx]
				switch container.Name {
				case "csi-provisioner":
					if imageVersion(container.Image) != "v2.0.4" {
						return apply(), nil
					}
				case "csi-attacher":
					if imageVersion(container.Image) != "v3.0.1" {
						return apply(), nil
					}
				case "csi-resizer":
					if imageVersion(container.Image) != "v1.0.1" {
						return apply(), nil
					}
				case "csi-snapshotter":
					if imageVersion(container.Image) != "v3.0.1" {
						return apply(), nil
					}
				case "gce-pd-driver":
					if imageVersion(container.Image) != "v1.2.0-gke.0" {
						return apply(), nil
					}
				}
			}
			return nil, nil

		},
		CloudController: infra.CloudControllerManager{
			Supported: true,
			CloudConfig: func(machine infra.Machine) io.Reader {
				instance := machine.(*instance)
				ctx := instance.context
				return strings.NewReader(fmt.Sprintf(`[Global]
project-id = "%s"
network-name = "%s"
node-instance-prefix = "orbos-"
multizone = false
local-zone = "%s"
container-api-endpoint = "Don't use container API'"
`,
					ctx.projectID,
					ctx.networkName,
					ctx.desired.Zone,
				))
			},
			ProviderName: "external",
		},
	}
}

func imageVersion(image string) string {
	return strings.Split(image, ":")[1]
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
