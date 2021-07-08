package kubernetes

import (
	"fmt"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/labels"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	mach "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func EnsureOrbiterArtifacts(
	monitor mntr.Monitor,
	apiLabels *labels.API,
	client *Client,
	pprof bool,
	orbiterversion string,
	imageRegistry string) error {

	monitor.WithFields(map[string]interface{}{
		"orbiter": orbiterversion,
	}).Debug("Ensuring orbiter artifacts")

	if orbiterversion == "" {
		return nil
	}

	nameLabels := toNameLabels(apiLabels, "orbiter")
	k8sNameLabels := labels.MustK8sMap(nameLabels)
	k8sPodSelector := labels.MustK8sMap(labels.DeriveNameSelector(nameLabels, false))

	cmd := []string{"/orbctl", "--gitops", "--orbconfig", "/etc/orbiter/orbconfig", "takeoff", "orbiter", "--recur", "--ingestion="}
	if pprof {
		cmd = append(cmd, "--pprof")
	}

	deployment := &apps.Deployment{
		ObjectMeta: mach.ObjectMeta{
			Name:      nameLabels.Name(),
			Namespace: "caos-system",
			Labels:    k8sNameLabels,
		},
		Spec: apps.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &mach.LabelSelector{
				MatchLabels: k8sPodSelector,
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: mach.ObjectMeta{
					Labels: labels.MustK8sMap(labels.AsSelectable(nameLabels)),
				},
				Spec: core.PodSpec{
					Containers: []core.Container{{
						Name:            "orbiter",
						ImagePullPolicy: core.PullIfNotPresent,
						Image:           fmt.Sprintf("%s/caos/orbos:%s", imageRegistry, orbiterversion),
						Command:         cmd,
						VolumeMounts: []core.VolumeMount{{
							Name:      "keys",
							ReadOnly:  true,
							MountPath: "/etc/orbiter",
						}},
						Ports: []core.ContainerPort{{
							Name:          "metrics",
							ContainerPort: 9000,
						}},
						Resources: core.ResourceRequirements{
							Limits: core.ResourceList{
								"cpu":    resource.MustParse("500m"),
								"memory": resource.MustParse("500Mi"),
							},
							Requests: core.ResourceList{
								"cpu":    resource.MustParse("250m"),
								"memory": resource.MustParse("250Mi"),
							},
						},
						LivenessProbe: &core.Probe{
							Handler: core.Handler{
								HTTPGet: &core.HTTPGetAction{
									Path:        "/health",
									Port:        intstr.FromInt(9000),
									Scheme:      core.URISchemeHTTP,
									HTTPHeaders: make([]core.HTTPHeader, 0, 0),
								},
							},
							InitialDelaySeconds: 10,
							TimeoutSeconds:      1,
							PeriodSeconds:       20,
							SuccessThreshold:    1,
							FailureThreshold:    3 * 5,
						},
					}},
					Volumes: []core.Volume{{
						Name: "keys",
						VolumeSource: core.VolumeSource{
							Secret: &core.SecretVolumeSource{
								SecretName: "caos",
								Optional:   boolPtr(false),
							},
						},
					}},
					NodeSelector: map[string]string{
						"node-role.kubernetes.io/master": "",
					},
					Tolerations: []core.Toleration{{
						Key:      "node-role.kubernetes.io/master",
						Effect:   "NoSchedule",
						Operator: "Exists",
					}, {
						Key:      fmt.Sprintf("%s%s", TaintKeyPrefix, Rebooting),
						Operator: "Exists",
						Effect:   "NoSchedule",
					}},
				},
			},
		},
	}

	if err := client.ApplyDeployment(deployment, true); err != nil {
		return err
	}
	monitor.WithFields(map[string]interface{}{
		"version": orbiterversion,
	}).Debug("Orbiter deployment ensured")

	if err := client.ApplyService(&core.Service{
		ObjectMeta: mach.ObjectMeta{
			Name:      nameLabels.Name(),
			Namespace: "caos-system",
			Labels:    k8sPodSelector,
		},
		Spec: core.ServiceSpec{
			Ports: []core.ServicePort{{
				Name:       "metrics",
				Protocol:   "TCP",
				Port:       9000,
				TargetPort: intstr.FromInt(9000),
			}},
			Selector: k8sPodSelector,
			Type:     core.ServiceTypeClusterIP,
		},
	}); err != nil {
		return err
	}
	monitor.Debug("Orbiter service ensured")

	patch := `
{
  "spec": {
    "template": {
      "spec": {
        "affinity": {
          "podAntiAffinity": {
            "preferredDuringSchedulingIgnoredDuringExecution": [{
              "weight": 100,
              "podAffinityTerm": {
                "topologyKey": "kubernetes.io/hostname"
              }
            }]
          }
        }
      }
    }
  }
}`
	if err := client.PatchDeployment("kube-system", "coredns", patch); err != nil {
		return err
	}

	monitor.Debug("CoreDNS deployment patched")
	return nil
}
