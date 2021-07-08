package main

import (
	"errors"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"

	"github.com/spf13/cobra"
)

func RebootCommand(getRv GetRootValues) *cobra.Command {
	return &cobra.Command{
		Use:   "reboot [<provider>.<pool>.<machine>] [<provider>.<pool>.<machine>]",
		Short: "Gracefully reboot machines",
		RunE: func(cmd *cobra.Command, args []string) (err error) {

			rv, err := getRv()
			if err != nil {
				return err
			}
			defer func() {
				err = rv.ErrFunc(err)
			}()

			monitor := rv.Monitor
			orbConfig := rv.OrbConfig
			gitClient := rv.GitClient

			if !rv.Gitops {
				return errors.New("reboot command is only supported with the --gitops flag and a committed orbiter.yml")
			}

			return requireMachines(monitor, gitClient, orbConfig, args, func(machine infra.Machine) (required bool, require func(), unrequire func()) {
				return machine.RebootRequired()
			})
		},
	}
}
