package cs

import (
	"context"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
	"github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/internal/ssh"
	"github.com/pkg/errors"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
)

func AdaptFunc(providerID, orbID string, whitelist dynamic.WhiteListFunc, orbiterCommit, repoURL, repoKey string, oneoff bool) orbiter.AdaptFunc {
	return func(monitor mntr.Monitor, finishedChan chan struct{}, desiredTree *tree.Tree, currentTree *tree.Tree) (queryFunc orbiter.QueryFunc, destroyFunc orbiter.DestroyFunc, configureFunc orbiter.ConfigureFunc, migrate bool, secrets map[string]*secret.Secret, err error) {
		defer func() {
			err = errors.Wrapf(err, "building %s failed", desiredTree.Common.Kind)
		}()
		desiredKind, err := parseDesired(desiredTree)
		if err != nil {
			return nil, nil, nil, migrate, nil, errors.Wrap(err, "parsing desired state failed")
		}
		desiredTree.Parsed = desiredKind
		secrets = make(map[string]*secret.Secret, 0)
		secret.AppendSecrets("", secrets, getSecretsMap(desiredKind))

		if desiredKind.Spec.RebootRequired == nil {
			desiredKind.Spec.RebootRequired = make([]string, 0)
			migrate = true
		}

		if desiredKind.Spec.Verbose && !monitor.IsVerbose() {
			monitor = monitor.Verbose()
		}

		if err := desiredKind.validateAdapt(); err != nil {
			return nil, nil, nil, migrate, nil, err
		}

		lbCurrent := &tree.Tree{}
		var lbQuery orbiter.QueryFunc

		lbQuery, lbDestroy, lbConfigure, migrateLocal, lbSecrets, err := loadbalancers.GetQueryAndDestroyFunc(monitor, whitelist, desiredKind.Loadbalancing, lbCurrent, finishedChan)
		if err != nil {
			return nil, nil, nil, migrate, nil, err
		}
		if migrateLocal {
			migrate = true
		}
		secret.AppendSecrets("", secrets, lbSecrets)

		svcFunc := func(ctx context.Context) *machinesService {
			return service(ctx, monitor, &desiredKind.Spec, orbID, providerID, oneoff)
		}

		current := &Current{
			Common: &tree.Common{
				Kind:    "orbiter.caos.ch/CloudScaleProvider",
				Version: "v0",
			},
		}
		currentTree.Parsed = current

		return func(ctx context.Context, nodeAgentsCurrent *common.CurrentNodeAgents, nodeAgentsDesired *common.DesiredNodeAgents, _ map[string]interface{}) (ensureFunc orbiter.EnsureFunc, err error) {
				defer func() {
					err = errors.Wrapf(err, "querying %s failed", desiredKind.Common.Kind)
				}()

				if err := desiredKind.validateQuery(); err != nil {
					return nil, err
				}

				svc := svcFunc(ctx)
				if err := svc.use(desiredKind.Spec.SSHKey); err != nil {
					return nil, err
				}

				if _, err := lbQuery(ctx, nodeAgentsCurrent, nodeAgentsDesired, nil); err != nil {
					return nil, err
				}

				_, naFuncs := core.NodeAgentFuncs(monitor, repoURL, repoKey)

				return query(&desiredKind.Spec, current, lbCurrent.Parsed, svc, nodeAgentsCurrent, nodeAgentsDesired, naFuncs, orbiterCommit)
			}, func(delegates map[string]interface{}) error {
				if err := lbDestroy(delegates); err != nil {
					return err
				}

				svc := svcFunc(context.Background())
				if err := svc.use(desiredKind.Spec.SSHKey); err != nil {
					return err
				}

				return destroy(svc, current)
			}, func(orb orb.Orb) error {

				if err := desiredKind.validateAPIToken(); err != nil {
					return err
				}

				if err := lbConfigure(orb); err != nil {
					return err
				}

				if desiredKind.Spec.SSHKey == nil ||
					desiredKind.Spec.SSHKey.Private == nil || desiredKind.Spec.SSHKey.Private.Value == "" ||
					desiredKind.Spec.SSHKey.Public == nil || desiredKind.Spec.SSHKey.Public.Value == "" {
					priv, pub, err := ssh.Generate()
					if err != nil {
						return err
					}
					desiredKind.Spec.SSHKey = &SSHKey{
						Private: &secret.Secret{Value: priv},
						Public:  &secret.Secret{Value: pub},
					}
				}

				svc := svcFunc(context.Background())
				if err := svc.use(desiredKind.Spec.SSHKey); err != nil {
					panic(err)
				}

				return core.ConfigureNodeAgents(svc, svc.cfg.monitor, orb)
			}, migrate, secrets, nil
	}
}
