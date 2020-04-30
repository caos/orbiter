package orbiter

import (
	"github.com/caos/orbos/internal/git"
	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/tree"
	"github.com/caos/orbos/mntr"
)

type DestroyFunc func() error

func Destroy(monitor mntr.Monitor, gitClient *git.Client, adapt AdaptFunc) error {

	trees, err := parse(gitClient, "orbiter.yml")
	if err != nil {
		return err
	}

	treeDesired := trees[0]
	treeCurrent := &tree.Tree{}

	_, destroy, _, err := adapt(monitor, treeDesired, treeCurrent)
	if err != nil {
		return err
	}

	if err := destroy(); err != nil {
		return err
	}

	monitor.OnChange = func(evt string, fields map[string]string) {
		if err := gitClient.UpdateRemote(mntr.CommitRecord([]*mntr.Field{{Key: "evt", Value: evt}}), git.File{
			Path:    "caos-internal/orbiter/current.yml",
			Content: []byte(""),
		}, git.File{
			Path:    "caos-internal/orbiter/node-agents-current.yml",
			Content: []byte(""),
		}, git.File{
			Path:    "caos-internal/orbiter/node-agents-desired.yml",
			Content: []byte(""),
		}, git.File{
			Path:    "orbiter.yml",
			Content: common.MarshalYAML(treeDesired),
		}); err != nil {
			panic(err)
		}
	}
	monitor.Changed("Orb destroyed")
	return nil
}
