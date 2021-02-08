package main

import (
	"context"

	"github.com/caos/orbos/internal/helpers"

	"github.com/caos/orbos/pkg/git"

	"github.com/spf13/cobra"

	"github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/mntr"
)

type RootValues func() (context.Context, mntr.Monitor, *orb.Orb, *git.Client, errFunc, error)

type errFunc func(err error) error

func RootCommand() (*cobra.Command, RootValues) {

	var (
		verbose       bool
		orbConfigPath string
	)

	cmd := &cobra.Command{
		Use:   "orbctl [flags]",
		Short: "Interact with your orbs",
		Long: `orbctl launches orbiters, booms, database-operators and networking-operators and simplifies common tasks such as updating your kubeconfig.
Participate in our community on https://github.com/caos/orbos
and visit our website at https://caos.ch`,
		Example: `$ mkdir -p ~/.orb
$ cat > ~/.orb/myorb << EOF
> url: git@github.com:me/my-orb.git
> masterkey: "$(gopass my-secrets/orbs/myorb/masterkey)"
> repokey: |
> $(cat ~/.ssh/myorbrepo | sed s/^/\ \ /g)
> EOF
$ orbctl -f ~/.orb/myorb [command]
`,
	}

	flags := cmd.PersistentFlags()
	flags.StringVarP(&orbConfigPath, "orbconfig", "f", "~/.orb/config", "Path to the file containing the orbs git repo URL, deploy key and the master key for encrypting and decrypting secrets")
	flags.BoolVar(&verbose, "verbose", false, "Print debug levelled logs")

	return cmd, func() (context.Context, mntr.Monitor, *orb.Orb, *git.Client, errFunc, error) {

		if verbose {
			monitor = monitor.Verbose()
		}

		prunedPath := helpers.PruneHome(orbConfigPath)
		orbConfig, err := orb.ParseOrbConfig(prunedPath)
		if err != nil {
			orbConfig = &orb.Orb{Path: prunedPath}
			return nil, mntr.Monitor{}, nil, nil, nil, err
		}

		return context.Background(), monitor, orbConfig, git.New(monitor, "orbos", "orbos@caos.ch"), func(err error) error {
			if err != nil {
				monitor.Error(err)
			}
			return nil
		}, nil
	}
}
