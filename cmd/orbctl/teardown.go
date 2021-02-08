package main

import (
	"fmt"
	"strings"

	"github.com/caos/orbos/pkg/labels"

	"github.com/caos/orbos/internal/api"
	"github.com/spf13/cobra"

	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/orb"
)

func TeardownCommand(rv RootValues) *cobra.Command {

	var (
		cmd = &cobra.Command{
			Use:   "teardown",
			Short: "Tear down an Orb",
			Long:  "Destroys a whole Orb",
			Aliases: []string{
				"shoot",
				"destroy",
				"devastate",
				"annihilate",
				"crush",
				"bulldoze",
				"total",
				"smash",
				"decimate",
				"kill",
				"trash",
				"wipe-off-the-map",
				"pulverize",
				"take-apart",
				"destruct",
				"obliterate",
				"disassemble",
				"explode",
				"blow-up",
			},
		}
	)

	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {
		ctx, monitor, orbConfig, gitClient, errFunc, err := rv()
		if err != nil {
			return err
		}
		defer func() {
			err = errFunc(err)
		}()

		if err := orbConfig.IsComplete(); err != nil {
			return err
		}

		if err := gitClient.Configure(ctx, orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
			return err
		}

		if err := gitClient.Clone(); err != nil {
			return err
		}

		foundOrbiter, err := api.ExistsOrbiterYml(gitClient)
		if err != nil {
			return err
		}
		if foundOrbiter {
			monitor.WithFields(map[string]interface{}{
				"version": version,
				"commit":  gitCommit,
				"repoURL": orbConfig.URL,
			}).Info("Destroying Orb")

			fmt.Println("Are you absolutely sure you want to destroy all clusters and providers in this Orb? [y/N]")
			var response string
			fmt.Scanln(&response)

			if !contains([]string{"y", "yes"}, strings.ToLower(response)) {
				monitor.Info("Not touching Orb")
				return nil
			}
			finishedChan := make(chan struct{})
			return orbiter.Destroy(
				monitor,
				gitClient,
				orb.AdaptFunc(
					labels.NoopOperator("ORBOS"),
					orbConfig,
					gitCommit,
					true,
					false,
					gitClient,
				),
				finishedChan,
			)
		}
		return nil
	}
	return cmd
}

func contains(slice []string, elem string) bool {
	for _, item := range slice {
		if elem == item {
			return true
		}
	}
	return false
}
