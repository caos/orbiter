package main

import (
	"github.com/caos/orbos/internal/api"
	"github.com/caos/orbos/internal/operator/database/kinds/orb"
	"github.com/caos/orbos/internal/start"
	kubernetes2 "github.com/caos/orbos/pkg/kubernetes"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

func RestoreCommand(rv RootValues) *cobra.Command {
	var (
		backup string
		cmd    = &cobra.Command{
			Use:   "restore",
			Short: "Restore from backup",
			Long:  "Restore from backup",
		}
	)

	flags := cmd.Flags()
	flags.StringVar(&backup, "backup", "", "Backup used for db restore")

	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {
		_, monitor, orbConfig, gitClient, errFunc, err := rv()
		if err != nil {
			return err
		}
		defer func() {
			err = errFunc(err)
		}()

		if err := orbConfig.IsConnectable(); err != nil {
			return err
		}

		if err := gitClient.Configure(orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
			return err
		}

		if err := gitClient.Clone(); err != nil {
			return err
		}

		found, err := api.ExistsDatabaseYml(gitClient)
		if err != nil {
			return err
		}
		if found {
			zitadel, err := api.ReadDatabaseYml(gitClient)
			if err != nil {
				return err
			}
			list, err := orb.BackupListFunc()(monitor, zitadel)
			if err != nil {
				return err
			}

			if backup == "" {
				prompt := promptui.Select{
					Label: "Select backup to restore",
					Items: list,
				}

				_, result, err := prompt.Run()
				if err != nil {
					return err
				}
				backup = result
			}

			kubeconfigs, err := start.GetKubeconfigs(monitor, gitClient, orbConfig, version)
			if err != nil {
				return err
			}
			for _, kubeconfig := range kubeconfigs {
				k8sClient := kubernetes2.NewK8sClient(monitor, &kubeconfig)
				if k8sClient.Available() {
					if err := start.DatabaseRestore(monitor, k8sClient, gitClient, backup); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	return cmd
}
