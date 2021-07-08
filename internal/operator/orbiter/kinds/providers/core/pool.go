package core

import (
	"sync"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
)

// TODO: Do we still need this?
type MachinesService interface {
	ListPools() ([]string, error)
	List(poolName string) (infra.Machines, error)
	Create(poolName string, desiredInstances int) (infra.Machines, error)
	DesiredMachines(poolName string, instances int) int
}

func Each(svc MachinesService, do func(pool string, machine infra.Machine) error) error {
	pools, err := svc.ListPools()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	for _, pool := range pools {
		machines, listErr := svc.List(pool)
		err = helpers.Concat(err, listErr)
		for idx := range machines {
			wg.Add(1)
			go func(p string, m infra.Machine) {
				defer wg.Done()
				err = helpers.Concat(err, do(p, m))
			}(pool, machines[idx])
		}
	}
	wg.Wait()
	return err
}
