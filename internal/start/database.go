package start

import (
	"context"
	"time"

	"github.com/caos/orbos/pkg/databases"

	"github.com/caos/orbos/internal/operator/database"
	"github.com/caos/orbos/internal/operator/database/kinds/orb"
	orbconfig "github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	kubernetes2 "github.com/caos/orbos/pkg/kubernetes"
)

func Database(monitor mntr.Monitor, orbConfigPath string, k8sClient *kubernetes2.Client, binaryVersion *string) error {
	takeoffChan := make(chan struct{})
	go func() {
		takeoffChan <- struct{}{}
	}()

	for range takeoffChan {
		orbConfig, err := orbconfig.ParseOrbConfig(orbConfigPath)
		if err != nil {
			monitor.Error(err)
			return err
		}

		gitClient := git.New(context.Background(), monitor, "orbos", "orbos@caos.ch")
		if err := gitClient.Configure(orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
			monitor.Error(err)
			return err
		}

		takeoff := database.Takeoff(monitor, gitClient, orb.AdaptFunc("", binaryVersion, "database", "backup"), k8sClient)

		go func() {
			started := time.Now()
			takeoff()

			monitor.WithFields(map[string]interface{}{
				"took": time.Since(started),
			}).Info("Iteration done")

			takeoffChan <- struct{}{}
		}()
	}

	return nil
}

func DatabaseBackup(monitor mntr.Monitor, orbConfigPath string, k8sClient *kubernetes2.Client, backup string, binaryVersion *string) error {
	orbConfig, err := orbconfig.ParseOrbConfig(orbConfigPath)
	if err != nil {
		monitor.Error(err)
		return err
	}

	gitClient := git.New(context.Background(), monitor, "orbos", "orbos@caos.ch")
	if err := gitClient.Configure(orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
		monitor.Error(err)
		return err
	}

	database.Takeoff(monitor, gitClient, orb.AdaptFunc(backup, binaryVersion, "instantbackup"), k8sClient)()
	return nil
}

func DatabaseRestore(monitor mntr.Monitor, k8sClient *kubernetes2.Client, gitClient *git.Client, timestamp string) error {
	return databases.Restore(monitor, k8sClient, gitClient, timestamp, []string{
		"notification",
		"adminapi",
		"auth",
		"authz",
		"eventstore",
		"management",
	})
}
