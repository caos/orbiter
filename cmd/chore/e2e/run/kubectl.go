package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type newKubectlCommandFunc func(context.Context) *exec.Cmd

func configureKubectl(kubeconfig string) newKubectlCommandFunc {
	return func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfig)
	}
}

type downloadKubeconfig func(context.Context, newOrbctlCommandFunc) (err error)

func downloadKubeconfigFunc(settings programSettings, to string) (downloadKubeconfig, func() error) {
	return func(ctx context.Context, orbctl newOrbctlCommandFunc) (err error) {

			readsecretCtx, readsecretCancel := context.WithTimeout(ctx, 10*time.Second)
			defer readsecretCancel()

			file, err := os.Create(to)
			if err != nil {
				return err
			}
			defer file.Close()

			return runCommand(settings, nil, file, nil, orbctl(readsecretCtx), "--gitops", "readsecret", fmt.Sprintf("orbiter.%s.kubeconfig.encrypted", settings.orbID))

		}, func() error {
			return os.Remove(to)
		}
}
