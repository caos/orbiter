package static

import (
	"context"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
	"github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/internal/ssh"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/mntr"
)

func AdaptFunc(id string, whitelist dynamic.WhiteListFunc, orbiterCommit, repoURL, repoKey string) orbiter.AdaptFunc {
	return func(monitor mntr.Monitor, finishedChan chan struct{}, desiredTree *tree.Tree, currentTree *tree.Tree) (queryFunc orbiter.QueryFunc, destroyFunc orbiter.DestroyFunc, configureFunc orbiter.ConfigureFunc, migrate bool, secrets map[string]*secret.Secret, err error) {
		defer func() {
			err = errors.Wrapf(err, "building %s failed", desiredTree.Common.Kind)
		}()
		desiredKind, err := parseDesiredV0(desiredTree)
		if err != nil {
			return nil, nil, nil, migrate, nil, errors.Wrap(err, "parsing desired state failed")
		}
		desiredTree.Parsed = desiredKind
		secrets = make(map[string]*secret.Secret, 0)
		secret.AppendSecrets("", secrets, getSecretsMap(desiredKind))

		if desiredKind.Spec.Verbose && !monitor.IsVerbose() {
			monitor = monitor.Verbose()
		}

		if desiredKind.Spec.ExternalInterfaces == nil {
			desiredKind.Spec.ExternalInterfaces = make([]string, 0)
			migrate = true
		}

		if err := desiredKind.validateAdapt(); err != nil {
			return nil, nil, nil, migrate, nil, err
		}

		lbCurrent := &tree.Tree{}
		var lbQuery orbiter.QueryFunc

		lbQuery, lbDestroy, lbConfigure, migrateLocal, lbsecrets, err := loadbalancers.GetQueryAndDestroyFunc(monitor, whitelist, desiredKind.Loadbalancing, lbCurrent, finishedChan)
		if err != nil {
			return nil, nil, nil, migrate, nil, err
		}
		if migrateLocal {
			migrate = true
		}
		secret.AppendSecrets("", secrets, lbsecrets)

		current := &Current{
			Common: &tree.Common{
				Kind:    "orbiter.caos.ch/StaticProvider",
				Version: "v0",
			},
		}
		currentTree.Parsed = current

		svcFunc := func(ctx context.Context) *machinesService {
			return newMachinesService(ctx, monitor, desiredKind, id)
		}
		return func(ctx context.Context, nodeAgentsCurrent *common.CurrentNodeAgents, nodeAgentsDesired *common.DesiredNodeAgents, _ map[string]interface{}) (ensureFunc orbiter.EnsureFunc, err error) {
				defer func() {
					err = errors.Wrapf(err, "querying %s failed", desiredKind.Common.Kind)
				}()

				if err := desiredKind.validateQuery(); err != nil {
					return nil, err
				}

				svc := svcFunc(ctx)
				lbQueryFunc := func() (orbiter.EnsureFunc, error) {
					return lbQuery(ctx, nodeAgentsCurrent, nodeAgentsDesired, nil)
				}

				if _, err := orbiter.QueryFuncGoroutine(lbQueryFunc); err != nil {
					return nil, err
				}

				if err := svc.updateKeys(); err != nil {
					return nil, err
				}

				queryFunc := func() (orbiter.EnsureFunc, error) {
					_, iterateNA := core.NodeAgentFuncs(monitor, repoURL, repoKey)
					return query(desiredKind, current, nodeAgentsDesired, nodeAgentsCurrent, lbCurrent.Parsed, monitor, svc, iterateNA, orbiterCommit)
				}
				return orbiter.QueryFuncGoroutine(queryFunc)
			}, func(delegates map[string]interface{}) error {
				if err := lbDestroy(delegates); err != nil {
					return err
				}

				svc := svcFunc(context.Background())
				if err := svc.updateKeys(); err != nil {
					return err
				}

				return destroy(svc, desiredKind, current)
			}, func(orb orb.Orb) error {
				if err := lbConfigure(orb); err != nil {
					return err
				}

				initKeys := desiredKind.Spec.Keys == nil
				if initKeys ||
					desiredKind.Spec.Keys.MaintenanceKeyPrivate == nil || desiredKind.Spec.Keys.MaintenanceKeyPrivate.Value == "" ||
					desiredKind.Spec.Keys.MaintenanceKeyPublic == nil || desiredKind.Spec.Keys.MaintenanceKeyPublic.Value == "" {
					priv, pub, err := ssh.Generate()
					if err != nil {
						return err
					}
					if initKeys {
						desiredKind.Spec.Keys = &Keys{}
					}
					desiredKind.Spec.Keys.MaintenanceKeyPrivate = &secret.Secret{Value: priv}
					desiredKind.Spec.Keys.MaintenanceKeyPublic = &secret.Secret{Value: pub}
					return nil
				}

				return core.ConfigureNodeAgents(svcFunc(context.Background()), monitor, orb)
			},
			migrate,
			secrets,
			nil
	}
}
