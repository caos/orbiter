package latest

import "github.com/caos/orbos/pkg/kubernetes/k8s"

type MetricsServer struct {
	//Flag if tool should be deployed
	//@default: false
	Deploy bool `json:"deploy" yaml:"deploy"`
	//Overwrite used image
	OverwriteImage string `json:"overwriteImage,omitempty" yaml:"overwriteImage,omitempty"`
	//Overwrite used image version
	OverwriteVersion string `json:"overwriteVersion,omitempty" yaml:"overwriteVersion,omitempty"`
	//NodeSelector for deployment
	NodeSelector map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`
	//Tolerations to run metrics server on nodes
	Tolerations k8s.Tolerations `json:"tolerations,omitempty" yaml:"tolerations,omitempty"`
	//Resource requirements
	Resources *k8s.Resources `json:"resources,omitempty" yaml:"resources,omitempty"`
}
