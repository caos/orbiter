package kubernetes

import (
	"fmt"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/labels"
	"gopkg.in/yaml.v3"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	mach "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func EnsureNetworkingArtifacts(
	monitor mntr.Monitor,
	apiLabels *labels.API,
	client ClientInt,
	version string,
	nodeselector map[string]string,
	tolerations []core.Toleration,
	imageRegistry string,
	gitops bool,
) error {

	monitor.WithFields(map[string]interface{}{
		"networking": version,
	}).Debug("Ensuring networking artifacts")

	if version == "" {
		return nil
	}

	nameLabels := toNameLabels(apiLabels, "networking-operator")
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

	if !gitops {
		crd := `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.2.2
  creationTimestamp: null
  name: networkings.caos.ch
spec:
  group: caos.ch
  names:
    kind: Networking
    listKind: NetworkingList
    plural: networkings
    singular: networking
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
          properties:
            kind:
              type: string
            networking:
              type: object
            spec:
              properties:
                customImageRegistry:
                  description: 'Use this registry to pull the Networking-operator
                    image from @default: ghcr.io'
                  type: string
                gitOps:
                  type: boolean
                nodeSelector:
                  additionalProperties:
                    type: string
                  type: object
                selfReconciling:
                  type: boolean
                tolerations:
                  items:
                    description: The pod this Toleration is attached to tolerates
                      any taint that matches the triple <key,value,effect> using the
                      matching operator <operator>.
                    properties:
                      effect:
                        description: Effect indicates the taint effect to match. Empty
                          means match all taint effects. When specified, allowed values
                          are NoSchedule, PreferNoSchedule and NoExecute.
                        type: string
                      key:
                        description: Key is the taint key that the toleration applies
                          to. Empty means match all taint keys. If the key is empty,
                          operator must be Exists; this combination means to match
                          all values and all keys.
                        type: string
                      operator:
                        description: Operator represents a key's relationship to the
                          value. Valid operators are Exists and Equal. Defaults to
                          Equal. Exists is equivalent to wildcard for value, so that
                          a pod can tolerate all taints of a particular category.
                        type: string
                      tolerationSeconds:
                        description: TolerationSeconds represents the period of time
                          the toleration (which must be of effect NoExecute, otherwise
                          this field is ignored) tolerates the taint. By default,
                          it is not set, which means tolerate the taint forever (do
                          not evict). Zero and negative values will be treated as
                          0 (evict immediately) by the system.
                        format: int64
                        type: integer
                      value:
                        description: Value is the taint value the toleration matches
                          to. If the operator is Exists, the value should be empty,
                          otherwise just a regular string.
                        type: string
                    type: object
                  type: array
                verbose:
                  type: boolean
                version:
                  type: string
              required:
              - selfReconciling
              - verbose
              type: object
            version:
              type: string
          required:
          - kind
          - networking
          - spec
          - version
          type: object
        status:
          type: object
      type: object
  version: v1
  versions:
  - name: v1
    served: true
    storage: true
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
		}).Debug("Networking Operator crd ensured")
	}

	var (
		cmd          = []string{"/orbctl", "takeoff", "networking", "--kubeconfig", ""}
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
				MatchLabels: labels.MustK8sMap(labels.DeriveNameSelector(nameLabels, false)),
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: mach.ObjectMeta{
					Labels: labels.MustK8sMap(labels.AsSelectable(nameLabels)),
				},
				Spec: core.PodSpec{
					ServiceAccountName: nameLabels.Name(),
					Containers: []core.Container{{
						Name:            "networking",
						ImagePullPolicy: core.PullIfNotPresent,
						Image:           fmt.Sprintf("%s/caos/orbos:%s", imageRegistry, version),
						Command:         cmd,
						Args:            []string{},
						Ports: []core.ContainerPort{{
							Name:          "metrics",
							ContainerPort: 2112,
							Protocol:      "TCP",
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
						VolumeMounts: volumeMounts,
					}},
					NodeSelector:                  nodeselector,
					Tolerations:                   tolerations,
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
	}).Debug("Networking Operator deployment ensured")

	return nil
}
