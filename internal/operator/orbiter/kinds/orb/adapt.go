package orb

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/labels"
	orbcfg "github.com/caos/orbos/pkg/orb"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/mntr"
)

func OperatorSelector() *labels.Selector {
	return labels.OpenOperatorSelector("ORBOS", "orbiter.caos.ch")
}

func AdaptFunc(
	operatorLabels *labels.Operator,
	orbConfig *orbcfg.Orb,
	orbiterCommit string,
	oneoff bool,
	deployOrbiter bool,
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

		desiredKind, err := ParseDesiredV0(desiredTree)
		if err != nil {
			return nil, nil, nil, migrate, nil, errors.Wrap(err, "parsing desired state failed")
		}
		desiredTree.Parsed = desiredKind
		secrets = make(map[string]*secret.Secret, 0)

		if err := desiredKind.validate(); err != nil {
			return nil, nil, nil, migrate, nil, err
		}

		if desiredKind.Spec.Verbose && !monitor.IsVerbose() {
			monitor = monitor.Verbose()
		}

		providerCurrents := make(map[string]*tree.Tree)
		providerQueriers := make([]orbiter.QueryFunc, 0)
		providerDestroyers := make([]orbiter.DestroyFunc, 0)
		providerConfigurers := make([]orbiter.ConfigureFunc, 0)

		whitelistChan := make(chan []*orbiter.CIDR)

		orbID, err := orbConfig.ID()
		if err != nil {
			panic(err)
		}

		for provID, providerTree := range desiredKind.Providers {

			providerCurrent := &tree.Tree{}
			providerCurrents[provID] = providerCurrent

			//			providermonitor := monitor.WithFields(map[string]interface{}{
			//				"provider": provID,
			//			})

			//			providerID := id + provID
			query, destroy, configure, migrateLocal, providerSecrets, err := providers.GetQueryAndDestroyFuncs(
				monitor,
				provID,
				providerTree,
				providerCurrent,
				whitelistChan,
				finishedChan,
				orbiterCommit,
				orbID,
				orbConfig.URL,
				orbConfig.Repokey,
				oneoff,
				desiredKind.Spec.PProf,
			)
			if err != nil {
				return nil, nil, nil, migrate, nil, err
			}

			if migrateLocal {
				migrate = true
			}

			providerQueriers = append(providerQueriers, query)
			providerDestroyers = append(providerDestroyers, destroy)
			providerConfigurers = append(providerConfigurers, configure)
			secret.AppendSecrets(provID, secrets, providerSecrets, nil, nil)
		}

		var provCurr map[string]interface{}
		destroyProviders := func(delegatedFromClusters map[string]interface{}) (map[string]interface{}, error) {
			if provCurr != nil {
				return provCurr, nil
			}

			provCurr = make(map[string]interface{})
			for _, destroyer := range providerDestroyers {
				if err := destroyer(delegatedFromClusters); err != nil {
					return nil, err
				}
			}

			for currKey, currVal := range providerCurrents {
				provCurr[currKey] = currVal.Parsed
			}
			return provCurr, nil
		}

		clusterCurrents := make(map[string]*tree.Tree)
		clusterQueriers := make([]orbiter.QueryFunc, 0)
		clusterDestroyers := make([]orbiter.DestroyFunc, 0)
		clusterConfigurers := make([]orbiter.ConfigureFunc, 0)
		for clusterID, clusterTree := range desiredKind.Clusters {

			clusterCurrent := &tree.Tree{}
			clusterCurrents[clusterID] = clusterCurrent
			query, destroy, configure, migrateLocal, clusterSecrets, err := clusters.GetQueryAndDestroyFuncs(
				monitor,
				operatorLabels,
				clusterID,
				clusterTree,
				oneoff,
				desiredKind.Spec.PProf,
				deployOrbiter,
				clusterCurrent,
				destroyProviders,
				whitelistChan,
				finishedChan,
				gitClient,
			)
			if err != nil {
				return nil, nil, nil, migrate, nil, err
			}
			clusterQueriers = append(clusterQueriers, query)
			clusterDestroyers = append(clusterDestroyers, destroy)
			clusterConfigurers = append(clusterConfigurers, configure)
			secret.AppendSecrets(clusterID, secrets, clusterSecrets, nil, nil)
			if migrateLocal {
				migrate = true
			}
		}

		currentTree.Parsed = &Current{
			Common: &tree.Common{
				Kind:    "orbiter.caos.ch/Orb",
				Version: "v0",
			},
			Clusters:  clusterCurrents,
			Providers: providerCurrents,
		}

		return func(nodeAgentsCurrent *common.CurrentNodeAgents, nodeAgentsDesired *common.DesiredNodeAgents, _ map[string]interface{}) (ensureFunc orbiter.EnsureFunc, err error) {

				providerEnsurers := make([]orbiter.EnsureFunc, 0)
				queriedProviders := make(map[string]interface{})
				for _, querier := range providerQueriers {
					ensurer, err := querier(nodeAgentsCurrent, nodeAgentsDesired, nil)

					if err != nil {
						return nil, err
					}
					providerEnsurers = append(providerEnsurers, ensurer)
				}

				for currKey, currVal := range providerCurrents {
					queriedProviders[currKey] = currVal.Parsed
				}

				clusterEnsurers := make([]orbiter.EnsureFunc, 0)
				for _, querier := range clusterQueriers {
					ensurer, err := querier(nodeAgentsCurrent, nodeAgentsDesired, queriedProviders)

					if err != nil {
						return nil, err
					}
					clusterEnsurers = append(clusterEnsurers, ensurer)
				}

				return func(psf func(monitor mntr.Monitor) error) *orbiter.EnsureResult {
					defer func() {
						err = errors.Wrapf(err, "ensuring %s failed", desiredKind.Common.Kind)
					}()

					done := true
					for _, ensurer := range append(providerEnsurers, clusterEnsurers...) {
						result := ensurer(psf)
						if result.Err != nil {
							return result
						}
						if !result.Done {
							done = false
						}
					}

					return orbiter.ToEnsureResult(done, nil)
				}, nil
			}, func(delegates map[string]interface{}) error {
				defer func() {
					err = errors.Wrapf(err, "destroying %s failed", desiredKind.Common.Kind)
				}()

				for _, destroyer := range clusterDestroyers {
					if err := destroyer(delegates); err != nil {
						return err
					}
				}
				return nil
			}, func(orb orbcfg.Orb) error {
				defer func() {
					err = errors.Wrapf(err, "ensuring %s failed", desiredKind.Common.Kind)
				}()

				for _, configure := range append(providerConfigurers, clusterConfigurers...) {
					if err := configure(orb); err != nil {
						return err
					}
				}
				return nil
			},
			migrate,
			secrets,
			nil
	}
}
