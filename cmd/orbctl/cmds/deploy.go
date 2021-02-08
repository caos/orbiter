package cmds

import (
	"context"

	"github.com/caos/orbos/internal/api"
	boomapi "github.com/caos/orbos/internal/operator/boom/api"
	cmdboom "github.com/caos/orbos/internal/operator/boom/cmd"
	orbnw "github.com/caos/orbos/internal/operator/networking/kinds/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/labels"
)

func deployBoom(ctx context.Context, monitor mntr.Monitor, gitClient *git.Client, kubeconfig *string, binaryVersion string) error {
	foundBoom, err := api.ExistsBoomYml(gitClient)
	if err != nil {
		return err
	}
	if !foundBoom {
		monitor.Info("No BOOM deployed as no boom.yml present")
		return nil
	}
	desiredTree, err := api.ReadBoomYml(gitClient)
	if err != nil {
		return err
	}

	// TODO: Parse toolset in cmdboom.Reconcile function (see deployDatabase, deployNetworking)
	desiredKind, _, _, apiKind, apiVersion, err := boomapi.ParseToolset(desiredTree)
	if err != nil {
		return err
	}

	k8sClient := kubernetes.NewK8sClient(ctx, monitor, kubeconfig)

	if desiredKind != nil &&
		desiredKind.Spec != nil &&
		desiredKind.Spec.Boom != nil &&
		desiredKind.Spec.Boom.Version != "" {
		binaryVersion = desiredKind.Spec.Boom.Version
	}

	if err := cmdboom.Reconcile(
		monitor,
		labels.MustForAPI(labels.MustForOperator("ORBOS", "boom.caos.ch", binaryVersion), apiKind, apiVersion),
		k8sClient,
		desiredKind.Spec.Boom,
		binaryVersion,
	); err != nil {
		return err
	}
	return nil
}
func deployNetworking(ctx context.Context, monitor mntr.Monitor, gitClient *git.Client, kubeconfig *string) error {
	found, err := api.ExistsNetworkingYml(gitClient)
	if err != nil {
		return err
	}
	if found {
		k8sClient := kubernetes.NewK8sClient(ctx, monitor, kubeconfig)

		if k8sClient.Available() {
			tree, err := api.ReadNetworkinglYml(gitClient)
			if err != nil {
				return err
			}

			if err := orbnw.Reconcile(monitor, tree)(k8sClient); err != nil {
				return err
			}
		} else {
			monitor.Info("Failed to connect to k8s")
		}
	}
	return nil
}
