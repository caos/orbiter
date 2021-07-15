package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"

	"github.com/caos/orbos/pkg/kubernetes"

	orbcfg "github.com/caos/orbos/pkg/orb"

	"github.com/caos/orbos/cmd/orbctl/cmds"
	"github.com/caos/orbos/internal/ctrlcrd"
	"github.com/caos/orbos/internal/ctrlgitops"
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func TakeoffCommand(getRv GetRootValues) *cobra.Command {
	var (
		verbose          bool
		recur            bool
		deploy           bool
		gitOpsBoom       bool
		gitOpsNetworking bool
		cmd              = &cobra.Command{
			Use:   "takeoff",
			Short: "Launch an orbiter",
			Long:  "Ensures a desired state",
		}
	)

	flags := cmd.Flags()
	flags.BoolVar(&recur, "recur", false, "Ensure the desired state continously")
	flags.BoolVar(&deploy, "deploy", true, "Ensure Orbiter and Boom deployments continously")
	flags.BoolVar(&gitOpsBoom, "gitops-boom", false, "Ensure Boom runs in gitops mode")
	flags.BoolVar(&gitOpsNetworking, "gitops-networking", false, "Ensure Networking-operator runs in gitops mode")

	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {

		rv, err := getRv("takeoff", "", map[string]interface{}{"recur": recur, "deploy": deploy, "gitops-boom": gitOpsBoom, "gitops-networking": gitOpsNetworking})
		if err != nil {
			return err
		}
		defer func() {
			err = rv.ErrFunc(err)
		}()

		orbConfig := rv.OrbConfig
		gitClient := rv.GitClient
		ctx := rv.Ctx

		return cmds.Takeoff(
			monitor,
			ctx,
			orbConfig,
			gitClient,
			recur,
			deploy,
			verbose,
			version,
			gitCommit,
			rv.Kubeconfig,
			rv.Gitops || gitOpsBoom,
			rv.Gitops || gitOpsNetworking,
		)
	}
	return cmd
}

func StartOrbiter(getRv GetRootValues) *cobra.Command {
	var (
		verbose bool
		recur   bool
		deploy  bool
		pprof   bool
		cmd     = &cobra.Command{
			Use:   "orbiter",
			Short: "Launch an orbiter",
			Long:  "Ensures a desired state",
		}
	)

	flags := cmd.Flags()
	flags.BoolVar(&recur, "recur", true, "Ensure the desired state continously")
	flags.BoolVar(&deploy, "deploy", true, "Ensure Orbiter deployment continously")
	flags.BoolVar(&pprof, "pprof", false, "Start pprof to analyse memory usage")

	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {

		rv, err := getRv("orbiter", "orbiter", map[string]interface{}{"recur": recur, "depoy": deploy, "pprof": pprof})
		if err != nil {
			return err
		}
		defer func() {
			err = rv.ErrFunc(err)
		}()

		monitor := rv.Monitor
		orbConfig := rv.OrbConfig
		gitClient := rv.GitClient
		ctx := rv.Ctx

		if err := orbcfg.IsComplete(orbConfig); err != nil {
			return err
		}

		if err := gitClient.Configure(orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
			return err
		}

		orbiterConfig := &ctrlgitops.OrbiterConfig{
			Recur:         recur,
			Deploy:        deploy,
			Verbose:       verbose,
			Version:       version,
			OrbConfigPath: orbConfig.Path,
			GitCommit:     gitCommit,
		}

		if pprof {
			go func() {
				log.Println(http.ListenAndServe("localhost:6060", nil))
			}()
		}

		return ctrlgitops.Orbiter(ctx, monitor, orbiterConfig, gitClient)
	}
	return cmd
}

func StartBoom(getRv GetRootValues) *cobra.Command {
	var (
		metricsAddr string
		cmd         = &cobra.Command{
			Use:   "boom",
			Short: "Launch a boom",
			Long:  "Ensures a desired state",
		}
	)

	flags := cmd.Flags()
	flags.StringVar(&metricsAddr, "metrics-addr", "", "The address the metric endpoint binds to.")

	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {

		rv, err := getRv("boom", "boom", map[string]interface{}{"metrics-addr": metricsAddr != ""})
		if err != nil {
			return err
		}
		defer func() {
			err = rv.ErrFunc(err)
		}()

		monitor := rv.Monitor
		orbConfig := rv.OrbConfig

		monitor.Info("Takeoff Boom")

		if rv.Gitops {
			return ctrlgitops.Boom(monitor, orbConfig.Path, version)
		} else {
			return ctrlcrd.Start(monitor, version, "/boom", metricsAddr, "", ctrlcrd.Boom)
		}
	}
	return cmd
}

func StartNetworking(getRv GetRootValues) *cobra.Command {
	var (
		metricsAddr string
		cmd         = &cobra.Command{
			Use:   "networking",
			Short: "Launch a networking operator",
			Long:  "Ensures a desired state of networking for an application",
		}
	)
	flags := cmd.Flags()
	flags.StringVar(&metricsAddr, "metrics-addr", "", "The address the metric endpoint binds to.")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		rv, err := getRv("networking-operator", "networking-operator", map[string]interface{}{"metrics-addr": metricsAddr != ""})
		if err != nil {
			return err
		}
		defer func() {
			err = rv.ErrFunc(err)
		}()

		monitor := rv.Monitor
		orbConfig := rv.OrbConfig

		monitor.Info("Takeoff Networking")

		if rv.Gitops {

			k8sClient, err := kubernetes.NewK8sClientWithPath(monitor, rv.Kubeconfig)
			if err != nil {
				return err
			}
			return ctrlgitops.Networking(monitor, orbConfig.Path, k8sClient, &version)
		} else {
			return ctrlcrd.Start(monitor, version, "/boom", metricsAddr, rv.Kubeconfig, ctrlcrd.Networking)
		}
		return nil
	}
	return cmd
}
