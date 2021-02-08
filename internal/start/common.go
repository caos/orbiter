package start

import (
	"time"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
)

func gitClient(monitor mntr.Monitor, task string) *git.Client {
	return git.New(monitor.WithField("task", task), "Boom", "boom@caos.ch")
}

func checks(monitor mntr.Monitor, client *git.Client) {
	t := time.NewTicker(13 * time.Hour)
	for range t.C {
		if err := client.Check(); err != nil {
			monitor.Error(err)
		}
	}
}
