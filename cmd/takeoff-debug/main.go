package main

import (
	"flag"

	"github.com/caos/orbos/cmd/orbctl/cmds"
	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/orb"
	orbcfg "github.com/caos/orbos/pkg/orb"
	"golang.org/x/net/context"
)

func main() {
	orbConfigPath := flag.String("orbconfig", "~/.orb/config", "The orbconfig file to use")
	kubeconfig := flag.String("kubeconfig", "~/.kube/config", "The kubeconfig file to use")
	gitops := flag.Bool("gitops", false, "Use gitops mode")
	verbose := flag.Bool("verbose", false, "Print debug levelled logs")

	flag.Parse()

	prunedPath := helpers.PruneHome(*orbConfigPath)
	orbConfig, err := orb.ParseOrbConfig(prunedPath)
	if err != nil {
		orbConfig = &orbcfg.Orb{Path: prunedPath}
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
		git.New(ctx, monitor, "orbos", "orbos@caos.ch"),
		false,
		false,
		*verbose,
		version,
		gitCommit,
		*kubeconfig,
		*gitops,
		*gitops,
	); err != nil {
		monitor.Error(err)
		panic(err)
	}
}
