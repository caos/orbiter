package kubernetes

import (
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/kubernetes"
)

func tryToConnect(monitor mntr.Monitor, desired *DesiredV0) *kubernetes.Client {
	var kc *string
	if desired.Spec.Kubeconfig != nil && desired.Spec.Kubeconfig.Value != "" {
		kc = &desired.Spec.Kubeconfig.Value
	}
	k8sClient, err := kubernetes.NewK8sClient(monitor, kc)
	if err != nil {
		// ignore
		err = nil
	}
	return k8sClient
}
