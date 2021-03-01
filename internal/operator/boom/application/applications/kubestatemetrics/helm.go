package kubestatemetrics

import (
	toolsetslatest "github.com/caos/orbos/internal/operator/boom/api/latest"
	"github.com/caos/orbos/internal/operator/boom/application/applications/kubestatemetrics/helm"
	"github.com/caos/orbos/internal/operator/boom/templator/helm/chart"
	"github.com/caos/orbos/internal/utils/helper"
	"github.com/caos/orbos/mntr"
)

func (k *KubeStateMetrics) SpecToHelmValues(monitor mntr.Monitor, toolset *toolsetslatest.ToolsetSpec) interface{} {

	imageTags := helm.GetImageTags()
	if toolset != nil && toolset.KubeMetricsExporter != nil {
		helper.OverwriteExistingValues(imageTags, map[string]string{
			"quay.io/coreos/kube-state-metrics": toolset.KubeMetricsExporter.OverwriteVersion,
		})
	}
	values := helm.DefaultValues(imageTags)

	spec := toolset.KubeMetricsExporter

	if spec == nil {
		return values
	}

	if spec.ReplicaCount != 0 {
		values.Replicas = spec.ReplicaCount
	}

	if spec.Affinity != nil {
		values.Affinity = spec.Affinity
	}

	if spec.NodeSelector != nil {
		for k, v := range spec.NodeSelector {
			values.NodeSelector[k] = v
		}
	}

	if spec.Tolerations != nil {
		for _, tol := range spec.Tolerations {
			values.Tolerations = append(values.Tolerations, tol)
		}
	}

	if spec.Resources != nil {
		values.Resources = spec.Resources
	}

	return values
}

func (k *KubeStateMetrics) GetChartInfo() *chart.Chart {
	return helm.GetChartInfo()
}

func (k *KubeStateMetrics) GetImageTags() map[string]string {
	return helm.GetImageTags()
}
