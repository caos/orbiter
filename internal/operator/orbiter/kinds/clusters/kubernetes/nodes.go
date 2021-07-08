package kubernetes

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/kubernetes"
)

func maintainNodes(allInitializedMachines initializedMachines, monitor mntr.Monitor, k8sClient *kubernetes.Client, pdf func(mntr.Monitor) error) (done bool, err error) {

	// Delete kubernetes nodes for unexisting machines
	if k8sClient != nil {
		nodes, err := k8sClient.ListNodes()
		if err != nil {
			return false, err
		}
		for nodeIdx := range nodes {
			node := nodes[nodeIdx]
			nodeName := node.GetName()

			leave := false
			for idx := range allInitializedMachines {
				machine := allInitializedMachines[idx]
				if machine.infra.ID() == nodeName {
					leave = true
					break
				}
			}
			if !leave {
				for idx := range node.Status.Conditions {
					condition := node.Status.Conditions[idx]
					if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
						return false, fmt.Errorf("there is no infrastructure machine corresponding to Kubernetes node %s, yet the node is still ready", nodeName)
					}
				}

				if err := k8sClient.DeleteNode(nodeName); err != nil {
					return false, err
				}
				monitor.WithField("node", nodeName).Info("Node deleted")
			}
		}
	}

	allInitializedMachines.forEach(monitor, func(machine *initializedMachine, machineMonitor mntr.Monitor) bool {
		if err = machine.reconcile(); err != nil {
			return false
		}
		return true
	})
	if err != nil {
		return false, err
	}

	allInitializedMachines.forEach(monitor, func(machine *initializedMachine, machineMonitor mntr.Monitor) bool {
		req, _, unreq := machine.infra.RebootRequired()
		if !req {
			return true
		}

		if machine.node != nil {
			if err = k8sClient.Drain(machine.currentMachine, machine.node, kubernetes.Rebooting, false); err != nil {
				return false
			}
		}
		machine.currentMachine.Rebooting = true
		machineMonitor.Info("Requiring reboot")
		unreq()
		machine.desiredNodeagent.RebootRequired = time.Now().Truncate(time.Minute)
		err = pdf(monitor.WithField("reason", "remove machine from reboot list"))
		return false
	})
	if err != nil {
		return false, err
	}

	done = true
	allInitializedMachines.forEach(monitor, func(machine *initializedMachine, machineMonitor mntr.Monitor) bool {
		if !machine.currentMachine.FirewallIsReady {
			done = false
			machineMonitor.Info("Node agent is not ready yet")
		}
		return true
	})
	return done, nil
}
