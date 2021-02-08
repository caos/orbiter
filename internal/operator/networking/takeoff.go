package networking

import (
	"errors"

	"github.com/caos/orbos/internal/operator/core"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/tree"
)

// TODO: Shouldn't gitClient be reconfigured in each iteration?
func Takeoff(monitor mntr.Monitor, gitClient *git.Client, adapt core.AdaptFunc, k8sClient *kubernetes.Client) func() {
	return func() {
		internalMonitor := monitor.WithField("operator", "networking")
		internalMonitor.Info("Takeoff")
		treeDesired, err := core.Parse(gitClient, "networking.yml")
		if err != nil {
			monitor.Error(err)
			return
		}
		treeCurrent := &tree.Tree{}

		if !k8sClient.Available() {
			internalMonitor.Error(errors.New("kubeclient is not available"))
			return
		}

		query, _, _, err := adapt(internalMonitor, treeDesired, treeCurrent)
		if err != nil {
			internalMonitor.Error(err)
			return
		}

		ensure, err := query(k8sClient, map[string]interface{}{})
		if err != nil {
			internalMonitor.Error(err)
			return
		}

		if err := ensure(k8sClient); err != nil {
			internalMonitor.Error(err)
			return
		}
	}
}
