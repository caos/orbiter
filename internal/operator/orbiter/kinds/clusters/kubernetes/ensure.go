package kubernetes

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/helper"
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

	done, err = ensureSoftware(
		monitor,
		targetVersion,
		k8sClient,
		controlplaneMachines,
		workerMachines)
	if err != nil || !done {
		monitor.Info("Upgrading is not done yet")
		return done, err
	}

	done, err = ensureUpScale(
		monitor,
		clusterID,
		desired,
		pdf,
		controlplane,
		workers,
		kubeAPIAddress,
		targetVersion,
		k8sClient,
		oneoff,
		func(created infra.Machine, pool *initializedPool) initializedMachine {
			machine := initializeMachine(created, pool)
			target := targetVersion.DefineSoftware()
			machine.desiredNodeagent.Software.Merge(target)
			return *machine
		},
		providerK8sSpec,
	)
	if err != nil {
		return done, err
	}

	if !done {
		monitor.Info("Scaling is not done yet")
	}

	// When bootstrapping, the admin config was just generated
	if helper.IsNil(k8sClient) {
		k8sClient = tryToConnect(monitor, desired)
	}

	return done, ensureK8sPlugins(monitor, gitClient, k8sClient, *desired, providerK8sSpec)
}
