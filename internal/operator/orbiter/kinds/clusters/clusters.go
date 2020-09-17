package clusters

import (
	"github.com/caos/orbos/internal/docu"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/kubernetes"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers"
	"github.com/caos/orbos/internal/secret"
	"github.com/caos/orbos/internal/tree"
	"github.com/caos/orbos/mntr"
	"github.com/pkg/errors"
)

const (
	kKind = "orbiter.caos.ch/KubernetesCluster"
)

func GetQueryAndDestroyFuncs(
	monitor mntr.Monitor,
	clusterID string,
	clusterTree *tree.Tree,
	oneoff bool,
	deployOrbiter bool,
	clusterCurrent *tree.Tree,
	destroyProviders func() (map[string]interface{}, error),
	whitelistChan chan []*orbiter.CIDR,
	finishedChan chan struct{},
) (
	orbiter.QueryFunc,
	orbiter.DestroyFunc,
	orbiter.ConfigureFunc,
	bool,
	map[string]*secret.Secret,
	error,
) {

	switch clusterTree.Common.Kind {
	case kKind:
		adaptFunc := func() (orbiter.QueryFunc, orbiter.DestroyFunc, orbiter.ConfigureFunc, bool, map[string]*secret.Secret, error) {
			return kubernetes.AdaptFunc(
				clusterID,
				oneoff,
				deployOrbiter,
				destroyProviders,
				func(whitelist []*orbiter.CIDR) {
					go func() {
						monitor.Debug("Sending whitelist")
						whitelistChan <- whitelist
						close(whitelistChan)
					}()
					monitor.Debug("Whitelist sent")
				},
			)(
				monitor.WithFields(map[string]interface{}{"cluster": clusterID}),
				finishedChan,
				clusterTree,
				clusterCurrent,
			)
		}
		return orbiter.AdaptFuncGoroutine(adaptFunc)
		//				subassemblers[provIdx] = static.New(providerPath, generalOverwriteSpec, staticadapter.New(providermonitor, providerID, "/healthz", updatesDisabled, cfg.NodeAgent))
	default:
		return nil, nil, nil, false, nil, errors.Errorf("unknown cluster kind %s", clusterTree.Common.Kind)
	}
}

func GetDocuInfo() []*docu.Type {
	path, kVersions := kubernetes.GetDocuInfo()
	typeList := []*docu.Type{{
		Name: "clusters",
		Kinds: []*docu.Info{
			{
				Path:     path,
				Kind:     kKind,
				Versions: kVersions,
			},
		},
	}}
	return append(loadbalancers.GetDocuInfo(), typeList...)
}
