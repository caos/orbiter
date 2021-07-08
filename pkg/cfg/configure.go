package cfg

import (
	"fmt"

	"github.com/caos/orbos/pkg/helper"

	nwOrb "github.com/caos/orbos/internal/operator/networking/kinds/orb"

	"github.com/caos/orbos/internal/operator/boom/api"
	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	orbiterOrb "github.com/caos/orbos/internal/operator/orbiter/kinds/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/labels"
	"github.com/caos/orbos/pkg/orb"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
	"gopkg.in/yaml.v3"
)

func ApplyOrbconfigSecret(
	orbConfig *orb.Orb,
	k8sClient kubernetes.ClientInt,
	monitor mntr.Monitor,
) error {

	if helper.IsNil(k8sClient) {
		monitor.Info("Writing new orbconfig skipped as no kubernetes cluster connection is available")
		return nil
	}

	monitor.Info("Writing orbconfig kubernetes secret")

	orbConfigBytes, err := yaml.Marshal(orbConfig)
	if err != nil {
		return err
	}

	if err := kubernetes.EnsureOrbconfigSecret(monitor, k8sClient, orbConfigBytes); err != nil {
		return fmt.Errorf("writing orbconfig kubernetes secret failed: %w", err)
	}

	monitor.Info("Orbconfig kubernetes secret written")
	return nil
}

func ConfigureOperators(
	gitClient *git.Client,
	rewriteKey string,
	configurers []func() (func() git.File, error),
) error {

	marshallers := make([]func() git.File, 0)

	for i := range configurers {
		marshaller, err := configurers[i]()
		if err != nil {
			return err
		}
		if marshaller == nil {
			continue
		}

		marshallers = append(marshallers, marshaller)
	}

	return secret.Rewrite(
		rewriteKey,
		func() error {
			return gitClient.UpdateRemote("Reconfigured operators", func() []git.File {
				gitFiles := make([]git.File, len(marshallers))
				for i := range marshallers {
					gitFiles[i] = marshallers[i]()
				}
				return gitFiles
			})
		},
	)
}

func ORBOSConfigurers(
	monitor mntr.Monitor,
	orbConfig *orb.Orb,
	gitClient *git.Client,

) []func() (func() git.File, error) {

	return []func() (func() git.File, error){
		OperatorConfigurer(
			git.OrbiterFile,
			monitor,
			gitClient,
			func() (*tree.Tree, interface{}, error) {
				_, _, configure, _, desired, _, _, err := orbiter.Adapt(gitClient, monitor, make(chan struct{}), orbiterOrb.AdaptFunc(
					labels.NoopOperator("ORBOS"),
					orbConfig,
					"unknown",
					true,
					false,
					gitClient,
				))
				if err != nil {
					return nil, nil, err
				}

				return desired, desired.Parsed, configure(*orbConfig)
			},
		),
		OperatorConfigurer(
			git.BoomFile,
			monitor,
			gitClient,
			func() (*tree.Tree, interface{}, error) {
				desired, err := gitClient.ReadTree(git.BoomFile)
				if err != nil {
					return nil, nil, err
				}
				toolset, _, _, _, err := api.ParseToolset(desired)
				return desired, toolset, err
			},
		),
		OperatorConfigurer(
			git.NetworkingFile,
			monitor,
			gitClient,
			func() (*tree.Tree, interface{}, error) {
				desired, err := gitClient.ReadTree(git.NetworkingFile)
				if err != nil {
					return nil, nil, err
				}

				_, _, _, _, _, err = nwOrb.AdaptFunc(nil, true)(monitor, desired, &tree.Tree{})
				return desired, desired.Parsed, err
			},
		),
	}

}

func OperatorConfigurer(
	desiredFile git.DesiredFile,
	monitor mntr.Monitor,
	gitClient *git.Client,
	configure func() (desired *tree.Tree, parsed interface{}, err error),
) func() (func() git.File, error) {
	return func() (func() git.File, error) {
		return configureOperator(
			desiredFile,
			monitor,
			gitClient,
			configure,
		)
	}
}

func configureOperator(
	desiredFile git.DesiredFile,
	monitor mntr.Monitor,
	gitClient *git.Client,
	configure func() (desired *tree.Tree, parsed interface{}, err error),
) (func() git.File, error) {

	if !gitClient.Exists(desiredFile) {
		return nil, nil
	}

	monitor.WithField("operator", desiredFile.WOExtension()).Info("Reconfiguring")

	tree, parsed, err := configure()
	if err != nil {
		return nil, err
	}

	tree.Parsed = parsed

	return func() git.File {
		return git.File{
			Path:    string(desiredFile),
			Content: common.MarshalYAML(tree),
		}
	}, nil
}
