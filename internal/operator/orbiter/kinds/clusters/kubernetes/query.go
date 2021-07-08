package kubernetes

import (
	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/pkg/errors"
)

func query(
	monitor mntr.Monitor,
	clusterID string,
	desired *DesiredV0,
	current *CurrentCluster,
	providerCurrents map[string]interface{},
	nodeAgentsCurrent *common.CurrentNodeAgents,
	nodeAgentsDesired *common.DesiredNodeAgents,
	k8sClient *kubernetes.Client,
	oneoff bool,
	gitClient *git.Client,
) (orbiter.EnsureFunc, error) {

	cloudPools, kubeAPIAddress, providerK8sSpec, privateInterface, err := GetProviderInfos(desired, providerCurrents)
	if err != nil {
		panic(err)
	}

	if err := poolIsConfigured(&desired.Spec.ControlPlane, cloudPools); err != nil {
		return nil, err
	}

	for _, w := range desired.Spec.Workers {
		if err := poolIsConfigured(w, cloudPools); err != nil {
			return nil, err
		}
	}

	controlplane, controlplaneMachines, workers, workerMachines, initializeMachine, uninitializeMachine, err := initialize(
		monitor,
		current,
		*desired,
		nodeAgentsCurrent,
		nodeAgentsDesired,
		cloudPools,
		k8sClient,
		func(machine *initializedMachine) {
			firewallFunc(monitor, *desired)(machine)
		})

	return func(psf func(mntr.Monitor) error) *orbiter.EnsureResult {
		return orbiter.ToEnsureResult(ensure(
			monitor,
			clusterID,
			desired,
			kubeAPIAddress,
			psf,
			k8sClient,
			oneoff,
			controlplane,
			controlplaneMachines,
			workers,
			workerMachines,
			initializeMachine,
			uninitializeMachine,
			gitClient,
			providerK8sSpec,
			privateInterface,
		))
	}, err
}

func poolIsConfigured(poolSpec *Pool, infra map[string]map[string]infra.Pool) error {
	prov, ok := infra[poolSpec.Provider]
	if !ok {
		return errors.Errorf("provider %s not configured", poolSpec.Provider)
	}
	if _, ok := prov[poolSpec.Pool]; !ok {
		return errors.Errorf("pool %s not configured on provider %s", poolSpec.Pool, poolSpec.Provider)
	}
	return nil
}

func GetProviderInfos(desired *DesiredV0, providerCurrents map[string]interface{}) (map[string]map[string]infra.Pool, *infra.Address, infra.Kubernetes, string, error) {
	cloudPools := make(map[string]map[string]infra.Pool)
	var (
		kubeAPIAddress   *infra.Address
		providerK8sSpec  infra.Kubernetes
		privateInterface string
	)

	for providerName, provider := range providerCurrents {
		if cloudPools[providerName] == nil {
			cloudPools[providerName] = make(map[string]infra.Pool)
		}
		prov := provider.(infra.ProviderCurrent)
		providerPools := prov.Pools()
		providerIngresses := prov.Ingresses()
		for providerPoolName, providerPool := range providerPools {
			cloudPools[providerName][providerPoolName] = providerPool
			if desired.Spec.ControlPlane.Provider == providerName && desired.Spec.ControlPlane.Pool == providerPoolName {
				var ok bool
				kubeAPIAddress, ok = providerIngresses["kubeapi"]
				if !ok {
					return nil, nil, providerK8sSpec, "", errors.New("no externally reachable address named kubeapi found")
				}
				providerK8sSpec = prov.Kubernetes()
				privateInterface = prov.PrivateInterface()
			}
		}
	}
	return cloudPools, kubeAPIAddress, providerK8sSpec, privateInterface, nil
}
