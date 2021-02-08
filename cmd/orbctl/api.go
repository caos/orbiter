package main

import (
	"github.com/caos/orbos/internal/api"
	boomapi "github.com/caos/orbos/internal/operator/boom/api"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/orb"
	"github.com/caos/orbos/pkg/labels"
	"github.com/spf13/cobra"
)

func APICommand(rv RootValues) *cobra.Command {
	var (
		cmd = &cobra.Command{
			Use:   "api",
			Short: "Upgrade the yml-files to the newest version",
			Long:  "Upgrade the yml-files to the newest version",
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
			_, _, _, migrate, desired, _, _, err := orbiter.Adapt(gitClient, monitor, make(chan struct{}), orb.AdaptFunc(
				labels.NoopOperator("ORBOS"),
				orbConfig,
				gitCommit,
				true,
				false,
				gitClient,
			))
			if err != nil {
				return err
			}

			if migrate {
				if err := api.PushOrbiterYml(monitor, "Update orbiter.yml", gitClient, desired); err != nil {
					return err
				}
			}

		}
		foundBoom, err := api.ExistsBoomYml(gitClient)
		if err != nil {
			return err
		}
		if foundBoom {

			desired, err := api.ReadBoomYml(gitClient)
			if err != nil {
				return err
			}

			toolset, migrate, _, _, _, err := boomapi.ParseToolset(desired)
			if err != nil {
				return err
			}
			if migrate {
				desired.Parsed = toolset
				if err := api.PushBoomYml(monitor, "Update boom.yml", gitClient, desired); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return cmd
}
