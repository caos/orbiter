package kubernetes

import (
	"fmt"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/kubernetes/k8s"
	"github.com/caos/orbos/pkg/labels"
	"gopkg.in/yaml.v3"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	mach "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func EnsureBoomArtifacts(
	monitor mntr.Monitor,
	apiLabels *labels.API,
	client ClientInt,
	version string,
	tolerations k8s.Tolerations,
	nodeselector map[string]string,
	resources *k8s.Resources,
	imageRegistry string,
	gitops bool,
) error {

	monitor.WithFields(map[string]interface{}{
		"boom": version,
	}).Debug("Ensuring boom artifacts")

	if version == "" {
		return nil
	}

	nameLabels := toNameLabels(apiLabels, "boom")
	k8sNameLabels := labels.MustK8sMap(nameLabels)

	if err := client.ApplyServiceAccount(&core.ServiceAccount{
		ObjectMeta: mach.ObjectMeta{
			Name:      nameLabels.Name(),
			Namespace: "caos-system",
			Labels:    k8sNameLabels,
		},
	}); err != nil {
		return err
	}

	if err := client.ApplyClusterRole(&rbac.ClusterRole{
		ObjectMeta: mach.ObjectMeta{
			Name:   nameLabels.Name(),
			Labels: k8sNameLabels,
		},
		Rules: []rbac.PolicyRule{{
			APIGroups: []string{"*"},
			Resources: []string{"*"},
			Verbs:     []string{"*"},
		}},
	}); err != nil {
		return err
	}

	if err := client.ApplyClusterRoleBinding(&rbac.ClusterRoleBinding{
		ObjectMeta: mach.ObjectMeta{
			Name:   nameLabels.Name(),
			Labels: k8sNameLabels,
		},

		RoleRef: rbac.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     nameLabels.Name(),
		},
		Subjects: []rbac.Subject{{
			Kind:      "ServiceAccount",
			Name:      nameLabels.Name(),
			Namespace: "caos-system",
		}},
	}); err != nil {
		return err
	}

	k8sPodSelector := labels.MustK8sMap(labels.DeriveNameSelector(nameLabels, false))

	if !gitops {
		crd := `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.2.2
  creationTimestamp: null
  name: booms.caos.ch
spec:
  group: caos.ch
  names:
    kind: Boom
    listKind: BoomList
    plural: booms
    singular: boom
  scope: ""
  validation:
    openAPIV3Schema:
      properties:
        apiVersion:
          description: 'APIVersion defines the versioned schema of this representation
            of an object. Servers should convert recognized schemas to the latest
            internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
          type: string
        kind:
          description: 'Kind is a string value representing the REST resource this
            object represents. Servers may infer this from the endpoint the client
            submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
          type: string
        metadata:
          type: object
        spec:
          type: object
        status:
          type: object
      type: object
  version: v1
  versions:
  - name: v1
    served: true
    storage: true
  - name: v1beta1
    served: true
    storage: false
  - name: v1beta2
    served: true
    storage: false
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []`

		crdDefinition := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(crd), &crdDefinition.Object); err != nil {
			return err
		}

		if err := client.ApplyCRDResource(
			crdDefinition,
		); err != nil {
			return err
		}
		monitor.WithFields(map[string]interface{}{
			"version": version,
		}).Debug("BOOM crd ensured")
	}

	var (
		cmd          = []string{"/orbctl", "takeoff", "boom", "--kubeconfig", ""}
		volumes      []core.Volume
		volumeMounts []core.VolumeMount
	)
	if gitops {
		cmd = append(cmd, "--gitops", "-f", "/secrets/orbconfig")
		volumes = []core.Volume{{
			Name: "orbconfig",
			VolumeSource: core.VolumeSource{
				Secret: &core.SecretVolumeSource{
					SecretName: "caos",
				},
			},
		}}
		volumeMounts = []core.VolumeMount{{
			Name:      "orbconfig",
			ReadOnly:  true,
			MountPath: "/secrets",
		}}
	}

	if _, _, ingestionEnabled := mntr.Environment(); !ingestionEnabled {
		cmd = append(cmd, "--disable-ingestion")
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
					ServiceAccountName: nameLabels.Name(),
					Containers: []core.Container{{
						Name:            "boom",
						ImagePullPolicy: core.PullIfNotPresent,
						Image:           fmt.Sprintf("%s/caos/orbos:%s", imageRegistry, version),
						Command:         cmd,
						Args:            []string{},
						Ports: []core.ContainerPort{{
							Name:          "metrics",
							ContainerPort: 2112,
							Protocol:      "TCP",
						}},
						VolumeMounts: volumeMounts,
						Resources:    core.ResourceRequirements(*resources),
					}},
					NodeSelector:                  nodeselector,
					Tolerations:                   tolerations.K8s(),
					Volumes:                       volumes,
					TerminationGracePeriodSeconds: int64Ptr(10),
				},
			},
		},
	}

	if err := client.ApplyDeployment(deployment, true); err != nil {
		return err
	}
	monitor.WithFields(map[string]interface{}{
		"version": version,
	}).Debug("BOOM deployment ensured")

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
				Port:       2112,
				TargetPort: intstr.FromInt(2112),
			}},
			Selector: k8sPodSelector,
			Type:     core.ServiceTypeClusterIP,
		},
	}); err != nil {
		return err
	}
	monitor.Debug("BOOM service ensured")

	return nil
}
