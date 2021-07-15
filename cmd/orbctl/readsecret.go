package main

import (
	"os"

	"github.com/caos/orbos/pkg/kubernetes/cli"

	"github.com/caos/orbos/pkg/secret"

	"github.com/caos/orbos/internal/secret/operators"

	"github.com/spf13/cobra"
)

func ReadSecretCommand(getRv GetRootValues) *cobra.Command {

	return &cobra.Command{
		Use:   "readsecret [path]",
		Short: "Print a secrets decrypted value to stdout",
		Long:  "Print a secrets decrypted value to stdout.\nIf no path is provided, a secret can interactively be chosen from a list of all possible secrets",
		Args:  cobra.MaximumNArgs(1),
		Example: `orbctl readsecret
orbctl readsecret orbiter.k8s.kubeconfig.encrypted
orbctl readsecret orbiter.k8s.kubeconfig.encrypted > ~/.kube/config`,
		RunE: func(cmd *cobra.Command, args []string) (err error) {

			path := ""
			if len(args) > 0 {
				path = args[0]
			}

			rv, err := getRv("readsecret", "", map[string]interface{}{"path": path})
			if err != nil {
				return err
			}
			defer func() {
				err = rv.ErrFunc(err)
			}()

			monitor := rv.Monitor
			orbConfig := rv.OrbConfig
			gitClient := rv.GitClient

			k8sClient, err := cli.Client(monitor, orbConfig, gitClient, rv.Kubeconfig, rv.Gitops, true)
			if err != nil && !rv.Gitops {
				return err
			}
			err = nil

			value, err := secret.Read(
				k8sClient,
				path,
				operators.GetAllSecretsFunc(monitor, path == "", rv.Gitops, gitClient, k8sClient, orbConfig),
			)
			if err != nil {
				return err
			}
			if _, err := os.Stdout.Write([]byte(value)); err != nil {
				panic(err)
			}
			return nil
		},
	}
}
