package kubernetes

import (
	"sync"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
)

func newMachines(pool infra.Pool, number int, desiredInstances int) (machines []infra.Machine, err error) {

	var wg sync.WaitGroup
	var it int
	for it = 0; it < number; it++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infraMachines, addErr := pool.AddMachine(desiredInstances)
			if addErr != nil {
				err = helpers.Concat(err, addErr)
				return
			}
			for _, machine := range infraMachines {
				machines = append(machines, machine)
			}
		}()
	}

	wg.Wait()

	if err != nil {
		for _, machine := range machines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err = helpers.Concat(err, machine.Remove())
			}()
		}
		wg.Wait()
	}

	return machines, err
}
