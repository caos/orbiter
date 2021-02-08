package kubernetes

import (
	"context"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/pkg/labels"
	core "k8s.io/api/core/v1"

	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
)

var deployErrors int

func AdaptFunc(
	apiLabels *labels.API,
	clusterID string,
	oneoff bool,
	deployOrbiter bool,
	destroyProviders func(map[string]interface{}) (map[string]interface{}, error),
	whitelist func(whitelist []*orbiter.CIDR),
	gitClient *git.Client,
) orbiter.AdaptFunc {

	return func(
		monitor mntr.Monitor,
		finishedChan chan struct{},
		desiredTree *tree.Tree,
		currentTree *tree.Tree,
	) (
		queryFunc orbiter.QueryFunc,
		destroyFunc orbiter.DestroyFunc,
		configureFunc orbiter.ConfigureFunc,
		migrate bool,
		secrets map[string]*secret.Secret,
		err error,
	) {
		defer func() {
			err = errors.Wrapf(err, "building %s failed", desiredTree.Common.Kind)
		}()

		if desiredTree.Common.Version != "v0" {
			migrate = true
		}

		desiredKind, err := parseDesiredV0(desiredTree)
		if err != nil {
			return nil, nil, nil, migrate, nil, errors.Wrap(err, "parsing desired state failed")
		}
		desiredTree.Parsed = desiredKind

		if desiredKind.Spec.ControlPlane.Taints == nil {
			taints := Taints([]Taint{{
				Key:    "node-role.kubernetes.io/master",
				Effect: core.TaintEffectNoSchedule,
			}})
			desiredKind.Spec.ControlPlane.Taints = &taints
			migrate = true
		}

		for _, workers := range desiredKind.Spec.Workers {
			if workers.Taints == nil {
				taints := Taints(make([]Taint, 0))
				workers.Taints = &taints
				migrate = true
			}
		}

		if err := desiredKind.validate(); err != nil {
			return nil, nil, nil, migrate, nil, err
		}

		if desiredKind.Spec.Verbose && !monitor.IsVerbose() {
			monitor = monitor.Verbose()
		}

		whitelist([]*orbiter.CIDR{&desiredKind.Spec.Networking.PodCidr})

		var kc *string
		if desiredKind.Spec.Kubeconfig != nil && desiredKind.Spec.Kubeconfig.Value != "" {
			kc = &desiredKind.Spec.Kubeconfig.Value
		}
		dContext, dCancel := context.WithCancel(context.Background())
		defer dCancel()
		k8sClient := kubernetes.NewK8sClient(dContext, monitor, kc)

		if k8sClient.Available() && deployOrbiter {
			if err := kubernetes.EnsureCommonArtifacts(monitor, k8sClient); err != nil {
				deployErrors++
				monitor.WithFields(map[string]interface{}{
					"count": deployErrors,
					"error": err.Error(),
				}).Info("Applying Common failed, awaiting next iteration")
			}
			if deployErrors > 50 {
				panic(err)
			}

			imageRegistry := desiredKind.Spec.CustomImageRegistry
			if imageRegistry == "" {
				imageRegistry = "ghcr.io"
			}

			if err := kubernetes.EnsureOrbiterArtifacts(
				monitor,
				apiLabels,
				k8sClient,
				desiredKind.Spec.Versions.Orbiter,
				imageRegistry,
			); err != nil {
				deployErrors++
				monitor.WithFields(map[string]interface{}{
					"count": deployErrors,
					"error": err.Error(),
				}).Info("Deploying Orbiter failed, awaiting next iteration")
			} else {
				if oneoff {
					monitor.Info("Deployed Orbiter takes over control")
					finishedChan <- struct{}{}
				}
				deployErrors = 0
			}
			if deployErrors > 50 {
				panic(err)
			}
		}
		dCancel()

		currentKind := "orbiter.caos.ch/KubernetesCluster"
		current := &CurrentCluster{}
		currentTree.Parsed = &Current{
			Common: tree.Common{
				Kind:    currentKind,
				Version: "v0",
			},
			Current: current,
		}

		return func(ctx context.Context, nodeAgentsCurrent *common.CurrentNodeAgents, nodeAgentsDesired *common.DesiredNodeAgents, providers map[string]interface{}) (orbiter.EnsureFunc, error) {
				ensureFunc, err := query(
					monitor,
					clusterID,
					desiredKind,
					current,
					providers,
					nodeAgentsCurrent,
					nodeAgentsDesired,
					kubernetes.NewK8sClient(ctx, monitor, kc),
					oneoff,
					gitClient,
				)
				return ensureFunc, errors.Wrapf(err, "querying %s failed", desiredKind.Common.Kind)
			}, func(delegate map[string]interface{}) error {
				defer func() {
					err = errors.Wrapf(err, "destroying %s failed", desiredKind.Common.Kind)
				}()

				if k8sClient.Available() {
					volumes, err := k8sClient.ListPersistentVolumes()
					if err != nil {
						return err
					}

					volumeNames := make([]infra.Volume, len(volumes.Items))
					for idx := range volumes.Items {
						volumeNames[idx] = infra.Volume{Name: volumes.Items[idx].Name}
					}
					delegate[currentKind] = volumeNames
				}

				providers, err := destroyProviders(delegate)
				if err != nil {
					return err
				}

				desiredKind.Spec.Kubeconfig = nil

				destroyFunc := func() error {
					return destroy(providers, k8sClient)
				}

				return orbiter.DestroyFuncGoroutine(destroyFunc)
			},
			orbiter.NoopConfigure,
			migrate,
			getSecretsMap(desiredKind),
			nil
	}
}
