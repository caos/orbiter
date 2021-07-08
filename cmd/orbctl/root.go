package main

import (
	"context"
	"errors"
	"os"

	"github.com/caos/orbos/internal/helpers"

	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/orb"
	orbcfg "github.com/caos/orbos/pkg/orb"

	"github.com/spf13/cobra"

	"github.com/caos/orbos/mntr"
)

type RootValues struct {
	Ctx        context.Context
	Monitor    mntr.Monitor
	Gitops     bool
	OrbConfig  *orbcfg.Orb
	Kubeconfig string
	GitClient  *git.Client
	ErrFunc    errFunc
}

type GetRootValues func() (*RootValues, error)

type errFunc func(err error) error

var _ error = (*helpErr)(nil)

type helpErr struct {
	original error
}

func (u helpErr) Error() string { return u.original.Error() }

func (u *helpErr) Unwrap() error { return u.original }

func RootCommand() (*cobra.Command, GetRootValues) {

	ctx := context.Background()
	rv := &RootValues{
		Ctx: ctx,
		ErrFunc: func(err error) error {
			if err == nil {
				return nil
			}

			if err != nil {
				monitor.Error(err)
			}

			usageErr := helpErr{}
			if errors.As(err, &usageErr) {
				return err
			}
			os.Exit(1)
			return nil
		},
	}

	var (
		orbConfigPath string
		verbose       bool
	)

	cmd := &cobra.Command{
		Use:   "orbctl [flags]",
		Short: "Interact with your orbs",
		Long: `orbctl launches orbiters, booms networking-operators and simplifies common tasks such as updating your kubeconfig.
Participate in our community on https://github.com/caos/orbos
and visit our website at https://caos.ch`,
		Example: `$ # For being able to use the --gitops flag, you need to create an orbconfig and add an SSH deploy key to your github project 
$ # Create an ssh key pair
$ ssh-keygen -b 2048 -t rsa -f ~/.ssh/myorbrepo -q -N ""
$ # Create the orbconfig
$ mkdir -p ~/.orb
$ cat > ~/.orb/myorb << EOF
> # this is the ssh URL to your git repository
> url: git@github.com:me/my-orb.git
> masterkey: "$(openssl rand -base64 21)" # used for encrypting and decrypting secrets
> # the repokey is used to connect to your git repository
> repokey: |
> $(cat ~/.ssh/myorbrepo | sed s/^/\ \ /g)
> EOF
$ orbctl --gitops -f ~/.orb/myorb [command]
`,
		SilenceErrors: true,
	}

	flags := cmd.PersistentFlags()
	flags.StringVarP(&orbConfigPath, "orbconfig", "f", "~/.orb/config", "Path to the file containing the orbs git repo URL, deploy key and the master key for encrypting and decrypting secrets")
	flags.StringVarP(&rv.Kubeconfig, "kubeconfig", "k", "~/.kube/config", "Path to the kubeconfig file to the cluster orbctl should target")
	flags.BoolVar(&rv.Gitops, "gitops", false, "Run orbctl in gitops mode. Not specifying this flag is only supported for BOOM and Networking Operator")
	flags.BoolVar(&verbose, "verbose", false, "Print debug levelled logs")

	return cmd, func() (*RootValues, error) {

		if verbose {
			monitor = monitor.Verbose()
		}
		rv.Monitor = monitor
		rv.Kubeconfig = helpers.PruneHome(rv.Kubeconfig)
		rv.GitClient = git.New(ctx, monitor, "orbos", "orbos@caos.ch")

		var err error
		if rv.Gitops {
			prunedPath := helpers.PruneHome(orbConfigPath)
			rv.OrbConfig, err = orb.ParseOrbConfig(prunedPath)
			if rv.OrbConfig == nil {
				rv.OrbConfig = &orb.Orb{Path: prunedPath}
			}
		}

		return rv, err
	}
}
