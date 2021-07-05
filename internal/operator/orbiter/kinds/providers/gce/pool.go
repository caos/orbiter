package gce

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
	uuid "github.com/satori/go.uuid"
	"google.golang.org/api/compute/v1"
)

var _ infra.Pool = (*infraPool)(nil)

type infraPool struct {
	pool       string
	normalized []*normalizedLoadbalancer
	svc        *machinesService
	machines   core.MachinesService
}

func newInfraPool(pool string, svc *machinesService, normalized []*normalizedLoadbalancer, machines core.MachinesService) *infraPool {
	return &infraPool{
		pool:       pool,
		normalized: normalized,
		svc:        svc,
		machines:   machines,
	}
}

func (i *infraPool) EnsureMember(machine infra.Machine) error {
	return i.ensureMembers(machine)
}

func (i *infraPool) EnsureMembers() error {
	return i.ensureMembers(nil)
}

func (i *infraPool) ensureMembers(machine infra.Machine) error {

	allInstances, err := getAllInstances(i.svc)
	if err != nil {
		return err
	}

	poolInstances := allInstances[i.pool]

	for _, n := range i.normalized {
	destpoolLoop:
		for _, destPool := range n.targetPool.destPools {
			if destPool == i.pool {

				var addInstances []*instance
			addInstanceLoop:
				for _, instance := range poolInstances {
					if machine != nil && machine.ID() != instance.ID() {
						continue addInstanceLoop
					}
					for _, tpInstance := range n.targetPool.gce.Instances {
						if instance.url == tpInstance {
							continue addInstanceLoop
						}
					}
					addInstances = append(addInstances, instance)
				}

				if len(addInstances) == 0 {
					continue destpoolLoop
				}

				if err := operateFunc(
					n.targetPool.log("Adding instances to target pool", true, addInstances),
					computeOpCall(i.svc.context.client.TargetPools.
						AddInstance(
							i.svc.context.projectID,
							i.svc.context.desired.Region,
							n.targetPool.gce.Name,
							&compute.TargetPoolsAddInstanceRequest{Instances: instances(addInstances).refs()},
						).
						RequestId(uuid.NewV1().String()).
						Do),
					toErrFunc(n.targetPool.log("Instances added to target pool", false, addInstances)),
				)(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (i *infraPool) DesiredMembers(instances int) int {
	return i.machines.DesiredMachines(i.pool, instances)
}

func (i *infraPool) GetMachines() (infra.Machines, error) {
	return i.machines.List(i.pool)
}

func (i *infraPool) AddMachine(desiredInstances int) (infra.Machines, error) {
	return i.machines.Create(i.pool, desiredInstances)
}
