package main

import (
	"flag"

	"github.com/caos/orbos/cmd/orbctl/cmds"
	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"golang.org/x/net/context"
)

func main() {
	orbConfigPath := flag.String("orbconfig", "~/.orb/config", "The orbconfig file to use")
	kubeconfig := flag.String("kubeconfig", "~/.kube/config", "The kubeconfig file to use")
	verbose := flag.Bool("verbose", false, "Print debug levelled logs")

	flag.Parse()

	prunedPath := helpers.PruneHome(*orbConfigPath)
	orbConfig, err := orb.ParseOrbConfig(prunedPath)
	if err != nil {
		orbConfig = &orb.Orb{Path: prunedPath}
	}

	monitor := mntr.Monitor{
		OnInfo:   mntr.LogMessage,
		OnChange: mntr.LogMessage,
		OnError:  mntr.LogError,
	}

	if *verbose {
		monitor = monitor.Verbose()
	}
	ctx := context.Background()

	gitCommit := "2248eaec648c728d407ad72a7052f7a366b4087a"
	version := "zitadel"

	if err := cmds.Takeoff(
		monitor,
		ctx,
		orbConfig,
		git.New(monitor, "orbos", "orbos@caos.ch"),
		false,
		false,
		true,
		*verbose,
		"",
		string(version),
		string(gitCommit),
		*kubeconfig,
	); err != nil {
		monitor.Error(err)
		panic(err)
	}
}
