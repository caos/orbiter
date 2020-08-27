package cmd

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/kubernetes"
	"github.com/caos/orbos/mntr"
	"github.com/pkg/errors"
)

func Reconcile(monitor mntr.Monitor, k8sClient *kubernetes.Client, version string) error {
	recMonitor := monitor.WithField("version", version)

	if k8sClient.Available() {
		if err := kubernetes.EnsureBoomArtifacts(monitor, k8sClient, version); err != nil {
			recMonitor.Error(errors.Wrap(err, "Failed to deploy zitadel-operator into k8s-cluster"))
			return err
		}
		recMonitor.Info("Applied boom")
	} else {
		recMonitor.Info("Failed to connect to k8s")
	}

	return nil
}