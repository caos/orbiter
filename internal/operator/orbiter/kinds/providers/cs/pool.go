package cs

import (
	"errors"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
)

var _ infra.Pool = (*infraPool)(nil)

type infraPool struct {
	pool        string
	machinesSvc core.MachinesService
}

func newInfraPool(pool string, machinesSvc core.MachinesService) *infraPool {
	return &infraPool{
		pool:        pool,
		machinesSvc: machinesSvc,
	}
}

func (i *infraPool) DesiredMembers(instances int) int {
	return instances
}

func (i *infraPool) EnsureMember(infra.Machine) error {
	// Keepalived health checks should work
	return nil
}

func (i *infraPool) EnsureMembers() error {
	// Keepalived health checks should work
	return nil
}

func (i *infraPool) GetMachines() (infra.Machines, error) {
	return i.machinesSvc.List(i.pool)
}

func (i *infraPool) AddMachine(desiredInstances int) (infra.Machines, error) {
	machines, err := i.machinesSvc.Create(i.pool, desiredInstances)
	if err != nil {
		return nil, err
	}
	if machines == nil || len(machines) != 1 {
		return nil, errors.New("error while creating machine")
	}
	return machines, nil
}
