package kubernetes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/caos/orbos/pkg/kubernetes"

	macherrs "k8s.io/apimachinery/pkg/api/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	core "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
)

type initializedPool struct {
	upscaling   int
	downscaling []*initializedMachine
	infra       infra.Pool
	tier        Tier
	desired     Pool
	machines    func() ([]*initializedMachine, error)
}

func (i *initializedMachines) forEach(baseMonitor mntr.Monitor, do func(machine *initializedMachine, machineMonitor mntr.Monitor) (goon bool)) {
	if i == nil {
		return
	}
	for _, machine := range *i {
		if !do(machine, baseMonitor.WithField("machine", machine.infra.ID())) {
			break
		}
	}
}

type initializeFunc func(initializedPool, []*initializedMachine) error
type uninitializeMachineFunc func(id string)
type initializeMachineFunc func(machine infra.Machine, pool *initializedPool) *initializedMachine

func (i *initializedPool) enhance(initialize initializeFunc) {
	original := i.machines
	i.machines = func() ([]*initializedMachine, error) {
		machines, err := original()
		if err != nil {
			return nil, err
		}
		if err := initialize(*i, machines); err != nil {
			return nil, err
		}
		return machines, nil
	}
}

type initializedMachine struct {
	infra            infra.Machine
	reconcile        func() error
	currentNodeagent *common.NodeAgentCurrent
	desiredNodeagent *common.NodeAgentSpec
	currentMachine   *Machine
	pool             *initializedPool
	node             *v1.Node
}

func initialize(
	monitor mntr.Monitor,
	curr *CurrentCluster,
	desired DesiredV0,
	nodeAgentsCurrent *common.CurrentNodeAgents,
	nodeAgentsDesired *common.DesiredNodeAgents,
	providerPools map[string]map[string]infra.Pool,
	k8s *kubernetes.Client,
	postInit func(machine *initializedMachine)) (
	controlplane *initializedPool,
	controlplaneMachines []*initializedMachine,
	workers []*initializedPool,
	workerMachines []*initializedMachine,
	initializeMachine initializeMachineFunc,
	uninitializeMachine uninitializeMachineFunc,
	err error) {

	curr.Status = "running"

	initializePool := func(infraPool infra.Pool, desired Pool, tier Tier) (*initializedPool, error) {
		pool := &initializedPool{
			infra:   infraPool,
			tier:    tier,
			desired: desired,
		}
		pool.machines = func() ([]*initializedMachine, error) {
			infraMachines, err := infraPool.GetMachines()
			if err != nil {
				return nil, err
			}
			machines := make([]*initializedMachine, len(infraMachines))
			for i, infraMachine := range infraMachines {
				machines[i] = initializeMachine(infraMachine, pool)
				if machines[i].currentMachine.Updating || machines[i].currentMachine.Rebooting {
					curr.Status = "maintaining"
				}
			}
			sort.Sort(initializedMachines(machines))
			return machines, nil
		}

		machines, err := pool.machines()
		if err != nil {
			return pool, err
		}

		var replace initializedMachines
		var backReplacement int
		for _, machine := range machines {
			if req, _, _ := machine.infra.ReplacementRequired(); req {
				replace = append(replace, machine)
				continue
			}
			if machine.currentMachine.Joined {
				backReplacement++
			}
		}

		machinesPerDesired := 1
		if desired.Nodes > 0 {
			machinesPerDesired = pool.infra.DesiredMembers(desired.Nodes) / desired.Nodes
		}

		upscale := (desired.Nodes * machinesPerDesired) + len(replace) - len(machines)
		if upscale > 0 {
			pool.upscaling = upscale
			return pool, nil
		}

		if len(replace) > 0 {
			for backReplacement >= (desired.Nodes*machinesPerDesired) && len(replace) > 0 {
				backReplacement--
				pool.downscaling = append(pool.downscaling, replace[0])
				replace = replace[1:]
			}
			return pool, nil
		}

		pool.downscaling = machines[(desired.Nodes * machinesPerDesired):]

		return pool, nil
	}

	initializeMachine = func(machine infra.Machine, pool *initializedPool) *initializedMachine {

		current := &Machine{
			Metadata: MachineMetadata{
				Tier:     pool.tier,
				Provider: pool.desired.Provider,
				Pool:     pool.desired.Pool,
			},
		}

		var node *v1.Node
		if k8s != nil {
			var k8sNodeErr error
			node, k8sNodeErr = k8s.GetNode(machine.ID())
			if k8sNodeErr != nil {
				if macherrs.IsNotFound(k8sNodeErr) {
					node = nil
				} else {
					current.Unknown = true
				}
			}
		}

		naSpec, _ := nodeAgentsDesired.Get(machine.ID())
		naCurr, _ := nodeAgentsCurrent.Get(machine.ID())

		// Retry if kubeapi returns other error than "NotFound"

		reconcile := func() error { return nil }
		if node != nil && !current.Unknown {
			reconcile = reconcileNodeFunc(*node, monitor, pool.desired, k8s, pool.tier, naSpec, naCurr)
			current.Joined = true
			for _, cond := range node.Status.Conditions {
				if cond.Type == v1.NodeReady {
					current.Ready = true
					current.Updating = k8s.Tainted(node, kubernetes.Updating)
					current.Rebooting = k8s.Tainted(node, kubernetes.Rebooting)
				}
			}
		}

		curr.Machines.Set(machine.ID(), current)

		machineMonitor := monitor.WithField("machine", machine.ID())

		naSpec.ChangesAllowed = !pool.desired.UpdatesDisabled
		k8sSoftware := ParseString(desired.Spec.Versions.Kubernetes).DefineSoftware()

		if !softwareDefines(*naSpec.Software, k8sSoftware) {
			k8sSoftware.Merge(KubernetesSoftware(naCurr.Software))
			if !softwareContains(*naSpec.Software, k8sSoftware) {
				naSpec.Software.Merge(k8sSoftware)
				machineMonitor.Debug("Kubernetes software desired")
			}
		}

		initMachine := &initializedMachine{
			infra:            machine,
			currentNodeagent: naCurr,
			desiredNodeagent: naSpec,
			reconcile:        reconcile,
			currentMachine:   current,
			pool:             pool,
			node:             node,
		}

		postInit(initMachine)

		return initMachine
	}

	for providerName, provider := range providerPools {
	pools:
		for poolName, pool := range provider {
			if desired.Spec.ControlPlane.Provider == providerName && desired.Spec.ControlPlane.Pool == poolName {
				controlplane, err = initializePool(pool, desired.Spec.ControlPlane, Controlplane)
				if err != nil {
					return controlplane,
						controlplaneMachines,
						workers,
						workerMachines,
						initializeMachine,
						uninitializeMachine,
						err
				}
				controlplaneMachines, err = controlplane.machines()
				if err != nil {
					return controlplane,
						controlplaneMachines,
						workers,
						workerMachines,
						initializeMachine,
						uninitializeMachine,
						err
				}
				continue
			}

			for _, desiredPool := range desired.Spec.Workers {
				if providerName == desiredPool.Provider && poolName == desiredPool.Pool {
					workerPool, err := initializePool(pool, *desiredPool, Workers)
					if err != nil {
						return controlplane,
							controlplaneMachines,
							workers,
							workerMachines,
							initializeMachine,
							uninitializeMachine,
							err
					}
					workers = append(workers, workerPool)
					initializedWorkerMachines, err := workerPool.machines()
					if err != nil {
						return controlplane,
							controlplaneMachines,
							workers,
							workerMachines,
							initializeMachine,
							uninitializeMachine,
							err
					}
					workerMachines = append(workerMachines, initializedWorkerMachines...)
					continue pools
				}
			}
		}
	}

	for _, machine := range append(controlplaneMachines, workerMachines...) {
		if !machine.currentMachine.Ready {
			curr.Status = "degraded"
		}
		if machine.currentMachine.Updating || !machine.currentMachine.Joined || !machine.currentMachine.FirewallIsReady {
			curr.Status = "maintaining"
			break
		}
	}

	return controlplane,
		controlplaneMachines,
		workers,
		workerMachines,
		initializeMachine, func(id string) {
			nodeAgentsDesired.Delete(id)
			curr.Machines.Delete(id)
		}, nil
}

func reconcileNodeFunc(node v1.Node, monitor mntr.Monitor, pool Pool, k8s *kubernetes.Client, tier Tier, naSpec *common.NodeAgentSpec, naCurr *common.NodeAgentCurrent) func() error {
	n := &node
	reconcileNode := false
	reconcileMonitor := monitor.WithField("node", n.Name)
	handleMaybe := func(maybeMonitorFields map[string]interface{}) {
		if maybeMonitorFields != nil {
			reconcileNode = true
			reconcileMonitor = monitor.WithFields(maybeMonitorFields)
		}
	}

	handleMaybe(reconcileLabel(n, "orbos.ch/pool", pool.Pool))
	handleMaybe(reconcileLabel(n, "orbos.ch/tier", string(tier)))
	handleMaybe(reconcileTaints(n, pool, k8s, naSpec, naCurr))

	if !reconcileNode {
		return func() error { return nil }
	}
	return func() error {
		reconcileMonitor.Info("Reconciling node")
		return k8s.UpdateNode(n)
	}
}

func reconcileTaints(node *v1.Node, pool Pool, k8s *kubernetes.Client, naSpec *common.NodeAgentSpec, naCurr *common.NodeAgentCurrent) map[string]interface{} {
	desiredTaints := pool.Taints.ToK8sTaints()
	newTaints := append([]core.Taint{}, desiredTaints...)
	updateTaints := false

	// user defined taints
outer:
	for _, existing := range node.Spec.Taints {
		if strings.HasPrefix(existing.Key, "node.kubernetes.io/") || strings.HasPrefix(existing.Key, kubernetes.TaintKeyPrefix) {
			newTaints = append(newTaints, existing)
			continue
		}
		for _, des := range desiredTaints {
			if existing.Key == des.Key &&
				existing.Effect == des.Effect &&
				existing.Value == des.Value {
				continue outer
			}
		}
		updateTaints = true
		break
	}
	// internal taints
	if k8s.Tainted(node, kubernetes.Updating) && node.Labels["orbos.ch/updating"] == node.Status.NodeInfo.KubeletVersion {
		newTaints = k8s.RemoveFromTaints(newTaints, kubernetes.Updating)
		updateTaints = true
	}

	if k8s.Tainted(node, kubernetes.Rebooting) && naCurr.Booted.After(naSpec.RebootRequired) {
		newTaints = k8s.RemoveFromTaints(newTaints, kubernetes.Rebooting)
		updateTaints = true
	}

	if !updateTaints && len(node.Spec.Taints) == len(newTaints) {
		return nil
	}
	node.Spec.Taints = newTaints
	return map[string]interface{}{"taints": desiredTaints}
}

func reconcileLabel(node *v1.Node, key, value string) map[string]interface{} {
	if node.Labels[key] == value {
		return nil
	}
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels[key] = value
	return map[string]interface{}{
		fmt.Sprintf("label.%s", key): value,
	}
}
