package start

import (
	"context"
	"time"

	"github.com/caos/orbos/internal/operator/networking"
	"github.com/caos/orbos/internal/operator/networking/kinds/orb"
	orbconfig "github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	kubernetes2 "github.com/caos/orbos/pkg/kubernetes"
)

func Networking(monitor mntr.Monitor, orbConfigPath string, k8sClient *kubernetes2.Client, binaryVersion *string) error {
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

		gitClient := git.New(monitor, "orbos", "orbos@caos.ch")
		if err := gitClient.Configure(context.TODO(), orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
			monitor.Error(err)
			return err
		}

		takeoff := networking.Takeoff(monitor, gitClient, orb.AdaptFunc(binaryVersion), k8sClient)

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
