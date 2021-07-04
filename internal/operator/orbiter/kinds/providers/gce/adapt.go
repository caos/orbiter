package gce

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
	"github.com/caos/orbos/internal/ssh"
	orbcfg "github.com/caos/orbos/pkg/orb"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/mntr"
)

func AdaptFunc(
	providerID,
	orbID string,
	whitelist dynamic.WhiteListFunc,
	orbiterCommit,
	repoURL,
	repoKey string,
	oneoff bool,
	pprof bool,
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
		desiredKind, err := parseDesiredV0(desiredTree)
		if err != nil {
			return nil, nil, nil, migrate, nil, errors.Wrap(err, "parsing desired state failed")
		}
		desiredTree.Parsed = desiredKind
		secrets = make(map[string]*secret.Secret, 0)
		secret.AppendSecrets("", secrets, getSecretsMap(desiredKind), nil, nil)

		if desiredKind.Spec.RebootRequired == nil {
			desiredKind.Spec.RebootRequired = make([]string, 0)
			migrate = true
		}

		if desiredKind.Spec.Verbose && !monitor.IsVerbose() {
			monitor = monitor.Verbose()
		}

		for _, pool := range desiredKind.Spec.Pools {
			if pool.StorageDiskType == "" {
				pool.StorageDiskType = "pd-standard"
				migrate = true
			}
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
		secret.AppendSecrets("", secrets, lbSecrets, nil, nil)

		svcFunc := func() (*machinesService, error) {
			return service(monitor, &desiredKind.Spec, orbID, providerID, oneoff)
		}

		current := &Current{
			Common: &tree.Common{
				Kind:    "orbiter.caos.ch/GCEProvider",
				Version: "v0",
			},
		}
		currentTree.Parsed = current

		return func(nodeAgentsCurrent *common.CurrentNodeAgents, nodeAgentsDesired *common.DesiredNodeAgents, _ map[string]interface{}) (ensureFunc orbiter.EnsureFunc, err error) {
				defer func() {
					err = errors.Wrapf(err, "querying %s failed", desiredKind.Common.Kind)
				}()

				if err := desiredKind.validateQuery(); err != nil {
					return nil, err
				}

				svc, err := svcFunc()
				if err != nil {
					return nil, err
				}

				if err := svc.use(desiredKind.Spec.SSHKey); err != nil {
					return nil, err
				}

				if _, err := lbQuery(nodeAgentsCurrent, nodeAgentsDesired, nil); err != nil {
					return nil, err
				}

				_, naFuncs := core.NodeAgentFuncs(monitor, repoURL, repoKey, pprof)

				return query(&desiredKind.Spec, current, lbCurrent.Parsed, svc, nodeAgentsCurrent, nodeAgentsDesired, naFuncs, orbiterCommit)
			}, func(delegates map[string]interface{}) error {
				if err := lbDestroy(delegates); err != nil {
					return err
				}

				ctx, err := svcFunc()
				if err != nil {
					return err
				}
				return destroy(ctx, delegates)
			}, func(orb orbcfg.Orb) error {

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

				svc, err := svcFunc()
				if err != nil {
					return err
				}

				if err := desiredKind.validateJSONKey(); err != nil {
					// TODO: Create service account and write its json key to desiredKind.Spec.JSONKey and push repo
					// return err
					return nil
				}

				if err := svc.use(desiredKind.Spec.SSHKey); err != nil {
					panic(err)
				}

				return core.ConfigureNodeAgents(svc, svc.context.monitor, orb, pprof)
			},
			migrate,
			secrets,
			nil
	}
}
