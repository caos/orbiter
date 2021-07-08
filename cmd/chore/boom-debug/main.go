package main

import (
	"context"
	"flag"

	"github.com/caos/orbos/internal/ctrlcrd"

	"github.com/caos/orbos/pkg/git"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/boom"
	"github.com/caos/orbos/mntr"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {

	gitops := flag.Bool("gitops", false, "defines if the operator should run in gitops mode not crd mode")
	orbconfig := flag.String("orbconfig", "~/.orb/config", "The orbconfig file to use")
	verbose := flag.Bool("verbose", false, "Print debug levelled logs")
	metricsAddr := flag.String("metrics-addr", ":8080", "The address the metric endpoint binds to.")

	flag.Parse()

	monitor := mntr.Monitor{
		OnInfo:   mntr.LogMessage,
		OnChange: mntr.LogMessage,
		OnError:  mntr.LogError,
	}

	if *verbose {
		monitor = monitor.Verbose()
	}

	if !*gitops {
		if err := ctrlcrd.Start(monitor, "crdoperators", "./artifacts", *metricsAddr, "", ctrlcrd.Boom); err != nil {
			panic(err)
		}
	} else {

		ensure := git.New(context.Background(), monitor.WithField("task", "ensure"), "Boom", "boom@caos.ch")
		query := git.New(context.Background(), monitor.WithField("task", "query"), "Boom", "boom@caos.ch")

		ensure.Clone()
		query.Clone()

		takeoff, _ := boom.Takeoff(
			monitor,
			"./artifacts",
			helpers.PruneHome(*orbconfig),
			ensure, query,
		)

		for {
			takeoff()
		}
	}
}
