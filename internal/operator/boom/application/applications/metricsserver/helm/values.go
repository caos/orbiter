package helm

import "github.com/caos/orbos/pkg/kubernetes/k8s"

type Rbac struct {
	Create     bool `yaml:"create"`
	PspEnabled bool `yaml:"pspEnabled"`
}
type ServiceAccount struct {
	Create bool   `yaml:"create"`
	Name   string `yaml:"name,omitempty"`
}
type APIService struct {
	Create bool `yaml:"create"`
}
type HostNetwork struct {
	Enabled bool `yaml:"enabled"`
}
type Image struct {
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag"`
	PullPolicy string `yaml:"pullPolicy"`
}
type HTTPGet struct {
	Path   string `yaml:"path,omitempty"`
	Port   string `yaml:"port,omitempty"`
	Scheme string `yaml:"scheme,omitempty"`
}
type LivenessProbe struct {
	HTTPGet             *HTTPGet `yaml:"httpGet,omitempty"`
	InitialDelaySeconds int      `yaml:"initialDelaySeconds,omitempty"`
}
type ReadinessProbe struct {
	HTTPGet             *HTTPGet `yaml:"httpGet,omitempty"`
	InitialDelaySeconds int      `yaml:"initialDelaySeconds,omitempty"`
}
type Capabilities struct {
	Drop []string `yaml:"drop,omitempty"`
}
type SecurityContext struct {
	AllowPrivilegeEscalation bool          `yaml:"allowPrivilegeEscalation,omitempty"`
	Capabilities             *Capabilities `yaml:"capabilities,omitempty"`
	ReadOnlyRootFilesystem   bool          `yaml:"readOnlyRootFilesystem,omitempty"`
	RunAsGroup               int           `yaml:"runAsGroup,omitempty"`
	RunAsNonRoot             bool          `yaml:"runAsNonRoot,omitempty"`
	RunAsUser                int           `yaml:"runAsUser,omitempty"`
}
type Service struct {
	Annotations map[string]string `yaml:"annotations,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Port        int               `yaml:"port,omitempty"`
	Type        string            `yaml:"type,omitempty"`
}
type PodDisruptionBudget struct {
	Enabled        bool        `yaml:"enabled"`
	MinAvailable   interface{} `yaml:"minAvailable,omitempty"`
	MaxUnavailable interface{} `yaml:"maxUnavailable,omitempty"`
}
type Values struct {
	Rbac                *Rbac                `yaml:"rbac,omitempty"`
	ServiceAccount      *ServiceAccount      `yaml:"serviceAccount,omitempty"`
	APIService          *APIService          `yaml:"apiService,omitempty"`
	HostNetwork         *HostNetwork         `yaml:"hostNetwork,omitempty"`
	Image               *Image               `yaml:"image,omitempty"`
	ImagePullSecrets    []interface{}        `yaml:"imagePullSecrets,omitempty"`
	Args                []string             `yaml:"args,omitempty"`
	Resources           *k8s.Resources       `yaml:"resources,omitempty"`
	NodeSelector        map[string]string    `yaml:"nodeSelector,omitempty"`
	Tolerations         k8s.Tolerations      `yaml:"tolerations,omitempty"`
	Affinity            struct{}             `yaml:"affinity,omitempty"`
	Replicas            int                  `yaml:"replicas,omitempty"`
	ExtraContainers     []interface{}        `yaml:"extraContainers"`
	PodLabels           map[string]string    `yaml:"podLabels,omitempty"`
	PodAnnotations      map[string]string    `yaml:"podAnnotations,omitempty"`
	ExtraVolumeMounts   []interface{}        `yaml:"extraVolumeMounts,omitempty"`
	ExtraVolumes        []interface{}        `yaml:"extraVolumes,omitempty"`
	LivenessProbe       *LivenessProbe       `yaml:"livenessProbe,omitempty"`
	ReadinessProbe      *ReadinessProbe      `yaml:"readinessProbe,omitempty"`
	SecurityContext     *SecurityContext     `yaml:"securityContext,omitempty"`
	Service             *Service             `yaml:"service,omitempty"`
	PodDisruptionBudget *PodDisruptionBudget `yaml:"podDisruptionBudget,omitempty"`
}
