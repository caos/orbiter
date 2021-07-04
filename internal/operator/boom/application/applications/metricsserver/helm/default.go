package helm

func DefaultValues(imageTags map[string]string, image string) *Values {
	return &Values{
		Rbac: &Rbac{
			Create:     true,
			PspEnabled: false,
		},
		ServiceAccount: &ServiceAccount{
			Create: true,
			Name:   "",
		},
		APIService:  &APIService{Create: true},
		HostNetwork: &HostNetwork{Enabled: false},
		Image: &Image{
			Repository: image,
			Tag:        imageTags[image],
			PullPolicy: "IfNotPresent",
		},
		ImagePullSecrets: nil,
		Args: []string{
			"--kubelet-insecure-tls",
			"--kubelet-preferred-address-types=InternalIP,Hostname,InternalDNS,ExternalDNS,ExternalIP",
		},
		Resources:         nil,
		NodeSelector:      map[string]string{},
		Tolerations:       nil,
		Affinity:          struct{}{},
		Replicas:          1,
		ExtraContainers:   nil,
		PodLabels:         map[string]string{},
		PodAnnotations:    map[string]string{},
		ExtraVolumeMounts: nil,
		ExtraVolumes:      nil,
		LivenessProbe: &LivenessProbe{
			HTTPGet: &HTTPGet{
				Path:   "/healthz",
				Port:   "https",
				Scheme: "HTTPS",
			},
			InitialDelaySeconds: 20,
		},
		ReadinessProbe: &ReadinessProbe{
			HTTPGet: &HTTPGet{
				Path:   "/healthz",
				Port:   "https",
				Scheme: "HTTPS",
			},
			InitialDelaySeconds: 20,
		},
		SecurityContext: &SecurityContext{
			AllowPrivilegeEscalation: false,
			Capabilities:             &Capabilities{Drop: []string{"all"}},
			ReadOnlyRootFilesystem:   true,
			RunAsGroup:               10001,
			RunAsNonRoot:             true,
			RunAsUser:                10001,
		},
		Service: &Service{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
			Port:        443,
			Type:        "ClusterIP",
		},
		PodDisruptionBudget: &PodDisruptionBudget{
			Enabled:        false,
			MinAvailable:   nil,
			MaxUnavailable: nil,
		},
	}
}
