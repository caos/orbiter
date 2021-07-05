package core

import (
	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
)

func DesireOSNetworking(
	monitor mntr.Monitor,
	nodeAgentsDesired *common.DesiredNodeAgents,
	nodeAgentsCurrent *common.CurrentNodeAgents,
	service MachinesService,
	name string,
	vips map[string][]string,
) (
	bool,
	error,
) {
	done := true
	pools, err := service.ListPools()
	if err != nil {
		return false, err
	}

	for _, pool := range pools {
		poolMachines, err := service.List(pool)
		if err != nil {
			return false, err
		}
		for idx := range poolMachines {
			machine := poolMachines[idx]
			machineVips, ok := vips[machine.ID()]
			if ok && machineVips != nil && len(machineVips) > 0 {
				doneMachine, err := DesireOSNetworkingForMachine(monitor, nodeAgentsDesired, nodeAgentsCurrent, machine, name, machineVips)
				if err != nil {
					return false, err
				}
				if !doneMachine {
					done = false
				}
			}
		}
	}
	return done, nil
}

func DesireOSNetworkingForMachine(
	monitor mntr.Monitor,
	nodeAgentsDesired *common.DesiredNodeAgents,
	nodeAgentsCurrent *common.CurrentNodeAgents,
	machine infra.Machine,
	name string,
	ips []string,
) (
	bool,
	error,
) {
	done := true
	desireNodeAgent := func(machine infra.Machine, nw common.Networking) {
		machineMonitor := monitor.WithField("machine", machine.ID())
		deepNa, _ := nodeAgentsDesired.Get(machine.ID())
		deepNaCurr, _ := nodeAgentsCurrent.Get(machine.ID())

		deepNa.Networking.Merge(nw)
		machineMonitor.WithField("networking", nw.ToCurrent()).Debug("Desired Cloudscale Networking")
		if !nw.IsContainedIn(deepNaCurr.Networking) {
			machineMonitor.WithField("networking", deepNa.Networking.ToCurrent()).Info("Awaiting networking config")
			done = false
		}
	}

	desireNodeAgent(machine, common.Networking{
		Interfaces: map[string]*common.NetworkingInterface{
			name: {
				Type: "dummy",
				IPs:  ips,
			},
		},
	})
	return done, nil
}
