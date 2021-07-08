package kubernetes

import (
	"sync"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
)

func alignMachines(
	monitor mntr.Monitor,
	controlplanePool *initializedPool,
	workerPools []*initializedPool,
	initializeMachine func(infra.Machine, *initializedPool) initializedMachine,
) (bool, []*initializedMachine, error) {
	wCount := 0
	for _, w := range workerPools {
		wCount += w.desired.Nodes
	}
	monitor.WithFields(map[string]interface{}{
		"control_plane_nodes": controlplanePool.desired.Nodes,
		"worker_nodes":        wCount,
	}).Debug("Ensuring scale")

	var machines []*initializedMachine
	upscalingDone := true
	var (
		wg  sync.WaitGroup
		err error
	)
	alignPool := func(pool *initializedPool, ensured func(int)) {
		defer wg.Done()

		if pool.upscaling > 0 {
			upscalingDone = false
			machines, alignErr := newMachines(pool.infra, pool.upscaling, pool.desired.Nodes)
			if alignErr != nil {
				err = helpers.Concat(err, alignErr)
				return
			}
			for _, machine := range machines {
				initializeMachine(machine, pool)
			}
		}

		if err != nil {
			return
		}
		poolMachines, listErr := pool.machines()
		if listErr != nil {
			err = helpers.Concat(err, listErr)
			return
		}
		machines = append(machines, poolMachines...)
		if ensured != nil {
			ensured(len(poolMachines))
		}
	}

	var ensuredControlplane int
	wg.Add(1)
	go alignPool(controlplanePool, func(ensured int) {
		ensuredControlplane = ensured
	})

	var ensuredWorkers int
	for _, workerPool := range workerPools {
		wg.Add(1)
		go alignPool(workerPool, func(ensured int) {
			ensuredWorkers += ensured
		})
	}
	wg.Wait()
	if err != nil {
		return false, machines, err
	}

	if !upscalingDone {
		monitor.Info("Upscaled machines are not ready yet")
		return false, machines, nil
	}
	return true, machines, nil
}
