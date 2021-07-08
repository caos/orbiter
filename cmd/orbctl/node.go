package main

import (
	"fmt"
	"strings"

	orbcfg "github.com/caos/orbos/pkg/orb"

	"github.com/AlecAivazis/survey/v2"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/tree"
	"github.com/spf13/cobra"
)

func NodeCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "node [id] command",
		Short:   "Work with an orbs node",
		Example: `orbctl node <exec|reboot|replace> `,
		Aliases: []string{"nodes", "machine", "machines"},
		Args:    cobra.MinimumNArgs(1),
	}
}

func requireMachines(monitor mntr.Monitor, gitClient *git.Client, orbConfig *orbcfg.Orb, args []string, method func(machine infra.Machine) (required bool, require func(), unrequire func())) error {
	return machines(monitor, gitClient, orbConfig, func(machineIDs []string, machines map[string]infra.Machine, desired *tree.Tree) error {

		var selected bool
		if len(args) <= 0 {
			selected = true
			if err := survey.AskOne(&survey.MultiSelect{
				Message: "Select machines:",
				Options: machineIDs,
			}, &args, survey.WithValidator(survey.Required)); err != nil {
				return err
			}
		}

		var push bool
		for _, arg := range args {
			machine, found := machines[arg]
			if !found {

				if selected {
					panic(fmt.Errorf("selected machine %s not found", arg))
				}

				if strings.Count(arg, ".") != 2 {
					return helpErr{fmt.Errorf("machine id must have the format <provider>.<pool>.<machine>")}
				}

				return fmt.Errorf("machine %s not found", arg)
			}

			required, require, _ := method(machine)
			if !required {
				require()
				push = true
			}
		}

		if !push {
			monitor.Info("Nothing changed")
			return nil
		}
		return gitClient.PushDesiredFunc(git.OrbiterFile, desired)(monitor)
	})
}
