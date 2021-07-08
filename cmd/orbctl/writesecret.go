package main

import (
	"errors"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	"github.com/caos/orbos/internal/secret/operators"
	"github.com/caos/orbos/pkg/kubernetes/cli"
	"github.com/caos/orbos/pkg/secret"
)

func WriteSecretCommand(getRv GetRootValues) *cobra.Command {

	var (
		value string
		file  string
		stdin bool
		cmd   = &cobra.Command{
			Use:   "writesecret [path]",
			Short: "Encrypt a secret and push it to the repository",
			Long:  "Encrypt a secret and push it to the repository.\nIf no path is provided, a secret can interactively be chosen from a list of all possible secrets",
			Args:  cobra.MaximumNArgs(1),
			Example: `orbctl writesecret --file ~/.ssh/my-orb-bootstrap
orbctl writesecret --value $(cat ~/.ssh/my-orb-bootstrap)
orbctl writesecret mystaticprovider.bootstrapkey.encrypted --file ~/.ssh/my-orb-bootstrap
orbctl writesecret mystaticprovider.bootstrapkey_pub.encrypted --file ~/.ssh/my-orb-bootstrap.pub
orbctl writesecret mygceprovider.google_application_credentials_value.encrypted --value "$(cat $GOOGLE_APPLICATION_CREDENTIALS)" `,
		}
	)

	flags := cmd.Flags()
	flags.StringVar(&value, "value", "", "Secret value to encrypt")
	flags.StringVarP(&file, "file", "s", "", "File containing the value to encrypt")
	flags.BoolVar(&stdin, "stdin", false, "Value to encrypt is read from standard input")

	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {

		s, err := content(value, file, stdin)
		if err != nil {
			return err
		}

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

		path := ""
		if len(args) > 0 {
			path = args[0]
		}

		k8sClient, err := cli.Client(monitor, orbConfig, gitClient, rv.Kubeconfig, rv.Gitops, true)
		if err != nil && !rv.Gitops {
			return err
		}
		err = nil

		return secret.Write(
			monitor,
			k8sClient,
			path,
			s,
			"orbctl",
			version,
			operators.GetAllSecretsFunc(monitor, true, rv.Gitops, gitClient, k8sClient, orbConfig),
			operators.PushFunc(monitor, rv.Gitops, gitClient, k8sClient))
	}
	return cmd
}

func content(value string, file string, stdin bool) (string, error) {

	channels := 0
	if value != "" {
		channels++
	}
	if file != "" {
		channels++
	}
	if stdin {
		channels++
	}

	if channels != 1 {
		return "", errors.New("content must be provided eighter by value or by file path or by standard input")
	}

	if value != "" {
		return value, nil
	}

	readFunc := func() ([]byte, error) {
		return ioutil.ReadFile(file)
	}
	if stdin {
		readFunc = func() ([]byte, error) {
			return ioutil.ReadAll(os.Stdin)
		}
	}

	c, err := readFunc()
	if err != nil {
		panic(err)
	}
	return string(c), err
}
