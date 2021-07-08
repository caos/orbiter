package kubernetes

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
)

func ensure(
	monitor mntr.Monitor,
	clusterID string,
	desired *DesiredV0,
	kubeAPIAddress *infra.Address,
	pdf func(mntr.Monitor) error,
	k8sClient *kubernetes.Client,
	oneoff bool,
	controlplane *initializedPool,
	controlplaneMachines []*initializedMachine,
	workers []*initializedPool,
	workerMachines []*initializedMachine,
	initializeMachine initializeMachineFunc,
	uninitializeMachine uninitializeMachineFunc,
	gitClient *git.Client,
	providerK8sSpec infra.Kubernetes,
	privateInterface string,
) (done bool, err error) {

	desireFW := firewallFunc(monitor, *desired)
	for _, machine := range append(controlplaneMachines, workerMachines...) {
		desireFW(machine)
	}

	if err := scaleDown(append(workers, controlplane), k8sClient, uninitializeMachine, monitor, pdf); err != nil {
		return false, err
	}

	done, err = maintainNodes(append(controlplaneMachines, workerMachines...), monitor, k8sClient, pdf)
	if err != nil || !done {
		return done, err
	}

	targetVersion := ParseString(desired.Spec.Versions.Kubernetes)

	machinesDone, initializedMachines, err := alignMachines(
		monitor,
		controlplane,
		workers,
		func(created infra.Machine, pool *initializedPool) initializedMachine {
			machine := initializeMachine(created, pool)
			target := targetVersion.DefineSoftware()
			machine.desiredNodeagent.Software.Merge(target)
			return *machine
		},
	)
	if err != nil || !machinesDone {
		monitor.Info("Aligning machines is not done yet")
		return machinesDone, err
	}

	upgradingDone, err := ensureSoftware(
		monitor,
		targetVersion,
		k8sClient,
		controlplaneMachines,
		workerMachines)
	if err != nil || !upgradingDone {
		monitor.Info("Upgrading is not done yet")
		return upgradingDone, err
	}

	if scalingDone, err := ensureNodes(
		monitor,
		clusterID,
		desired,
		pdf,
		kubeAPIAddress,
		targetVersion,
		k8sClient,
		oneoff,
		gitClient,
		providerK8sSpec,
		initializedMachines,
		privateInterface,
	); err != nil || !scalingDone {
		monitor.Info("Scaling is not done yet")
		return scalingDone, err
	}

	for _, pool := range append(workers, controlplane) {
		if err := pool.infra.EnsureMembers(); err != nil {
			return false, err
		}
	}

	return true, nil
}
