package main

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"

	"github.com/spf13/cobra"
)

func RebootCommand(rv RootValues) *cobra.Command {
	return &cobra.Command{
		Use:   "reboot",
		Short: "Gracefully reboot machines",
		Long:  "Pass machine ids as arguments, omit arguments for selecting machines interactively",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			ctx, monitor, orbConfig, gitClient, errFunc, err := rv()
			if err != nil {
				return err
			}
			defer func() {
				err = errFunc(err)
			}()

			return requireMachines(ctx, monitor, gitClient, orbConfig, args, func(machine infra.Machine) (required bool, require func(), unrequire func()) {
				return machine.RebootRequired()
			})
		},
	}
}
