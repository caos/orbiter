package providers

import (
	"regexp"

	"github.com/caos/orbos/pkg/secret"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/cs"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"

	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/gce"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/static"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/tree"
	"github.com/pkg/errors"
)

var alphanum = regexp.MustCompile("[^a-zA-Z0-9]+")

func GetQueryAndDestroyFuncs(
	monitor mntr.Monitor,
	provID string,
	providerTree *tree.Tree,
	providerCurrent *tree.Tree,
	whitelistChan chan []*orbiter.CIDR,
	finishedChan chan struct{},
	orbiterCommit, orbID, repoURL, repoKey string,
	oneoff bool,
	pprof bool,
) (
	orbiter.QueryFunc,
	orbiter.DestroyFunc,
	orbiter.ConfigureFunc,
	bool,
	map[string]*secret.Secret,
	error,
) {

	monitor = monitor.WithFields(map[string]interface{}{"provider": provID})

	wlFunc := func() []*orbiter.CIDR {
		monitor.Debug("Reading whitelist")
		wl := <-whitelistChan
		return wl
	}

	switch providerTree.Common.Kind {
	case "orbiter.caos.ch/GCEProvider":
		return gce.AdaptFunc(
			provID,
			orbID,
			wlFunc,
			orbiterCommit, repoURL, repoKey,
			oneoff,
			pprof,
		)(
			monitor,
			finishedChan,
			providerTree,
			providerCurrent,
		)
	case "orbiter.caos.ch/CloudScaleProvider":
		return cs.AdaptFunc(
			provID,
			orbID,
			wlFunc,
			orbiterCommit, repoURL, repoKey,
			oneoff,
			pprof,
		)(
			monitor,
			finishedChan,
			providerTree,
			providerCurrent,
		)
	case "orbiter.caos.ch/StaticProvider":
		return static.AdaptFunc(
			provID,
			wlFunc,
			orbiterCommit,
			repoURL,
			repoKey,
			pprof,
		)(
			monitor.WithFields(map[string]interface{}{"provider": provID}),
			finishedChan,
			providerTree,
			providerCurrent)
	default:
		return nil, nil, nil, false, nil, errors.Errorf("unknown provider kind %s", providerTree.Common.Kind)
	}
}

func ListMachines(
	monitor mntr.Monitor,
	providerTree *tree.Tree,
	provID string,
	orbID string,
) (
	map[string]infra.Machine,
	error,
) {

	switch providerTree.Common.Kind {
	case "orbiter.caos.ch/GCEProvider":
		return gce.ListMachines(
			monitor,
			providerTree,
			orbID,
			provID,
		)
	case "orbiter.caos.ch/CloudScaleProvider":
		return cs.ListMachines(
			monitor,
			providerTree,
			orbID,
			provID,
		)
	case "orbiter.caos.ch/StaticProvider":
		return static.ListMachines(
			monitor,
			providerTree,
			provID,
		)
	default:
		return nil, errors.Errorf("unknown provider kind %s", providerTree.Common.Kind)
	}
}
