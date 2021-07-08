package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/common"
)

var _ testFunc = bootstrap

func bootstrap(_ *testSpecs, settings programSettings, conditions *conditions) interactFunc {

	conditions.testCase = nil
	conditions.orbiter = &condition{
		watcher: watch(15*time.Minute, orbiter),
		checks: func(checkCtx context.Context, newKubectl newKubectlCommandFunc, currentOrbiter currentOrbiter, nodeagents common.NodeAgentsCurrentKind) error {

			cluster, err := currentOrbiter.cluster(settings)
			if err != nil {
				return err
			}

			masters := conditions.desiredMasters()
			machines := masters + conditions.desiredWorkers()

			currentMachinesLen := uint8(len(cluster.Machines.M))

			if currentMachinesLen != machines {
				err = helpers.Concat(err, fmt.Errorf("current state has %d machines instead of %d", currentMachinesLen, machines))
			}

			for nodeagentID, nodeagent := range cluster.Machines.M {
				if !nodeagent.Ready ||
					!nodeagent.FirewallIsReady ||
					!nodeagent.Joined {
					err = helpers.Concat(err, fmt.Errorf("nodeagent %s has current states joined=%t, firewallIsReady=%t, ready=%t",
						nodeagentID,
						nodeagent.Ready,
						nodeagent.FirewallIsReady,
						nodeagent.Joined,
					))
				}
			}

			for nodeagentID, nodeagent := range nodeagents.Current.NA {
				if !nodeagent.NodeIsReady {
					err = helpers.Concat(err, fmt.Errorf("nodeagent %s has not reported readiness yet", nodeagentID))
				}
				if nodeagent.Software.Kubelet.Version != conditions.kubernetes.Versions.Kubernetes ||
					nodeagent.Software.Kubeadm.Version != conditions.kubernetes.Versions.Kubernetes ||
					nodeagent.Software.Kubectl.Version != conditions.kubernetes.Versions.Kubernetes {
					err = helpers.Concat(err, fmt.Errorf("nodeagent %s has current states kubelet=%s, kubeadm=%s, kubectl=%s instead of %s",
						nodeagentID,
						nodeagent.Software.Kubelet.Version,
						nodeagent.Software.Kubeadm.Version,
						nodeagent.Software.Kubectl.Version,
						conditions.kubernetes.Versions.Kubernetes,
					))
				}
			}

			if cluster.Status != "running" {
				err = helpers.Concat(err, fmt.Errorf("cluster status is %s", cluster.Status))
			}

			return helpers.Concat(err, helpers.Fanout([]func() error{
				func() error {
					return checkPodsAreReady(
						checkCtx,
						settings,
						newKubectl,
						"kube-system",
						"component in (etcd, kube-apiserver, kube-controller-manager, kube-scheduler)",
						masters*4,
					)
				},
				func() error {
					return checkPodsAreReady(
						checkCtx,
						settings,
						newKubectl,
						"kube-system",
						"k8s-app=kube-proxy",
						machines,
					)
				},
				func() error {
					return checkPodsAreReady(
						checkCtx,
						settings,
						newKubectl,
						"kube-system",
						"k8s-app=kube-dns",
						2,
					)
				},
				func() error {
					return checkPodsAreReady(
						checkCtx,
						settings,
						newKubectl,
						"kube-system",
						"k8s-app=calico-kube-controllers",
						1,
					)
				},
				func() error {
					return checkPodsAreReady(
						checkCtx,
						settings,
						newKubectl,
						"kube-system",
						"k8s-app=calico-node",
						machines,
					)
				},
			})())
		},
	}

	return func(ctx context.Context, step uint8, newOrbctl newOrbctlCommandFunc) error {

		takeoffTimeout := 20 * time.Minute
		takeoffCtx, takeoffCancel := context.WithTimeout(ctx, takeoffTimeout)
		defer takeoffCancel()

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		started := time.Now()
		go func() {
			for {
				select {
				case <-ticker.C:
					printProgress(orbctl, settings, fmt.Sprintf("%d (takeoff)", step), started, takeoffTimeout)
				case <-takeoffCtx.Done():
					return
				}
			}
		}()

		if err := runCommand(settings, orbctl.strPtr(), nil, nil, newOrbctl(takeoffCtx), "--gitops", "takeoff"); err != nil {
			return err
		}

		buf := new(bytes.Buffer)
		defer buf.Reset()

		if err := runCommand(settings, nil, buf, nil, newOrbctl(takeoffCtx), "--gitops", "readsecret", fmt.Sprintf("orbiter.%s.kubeconfig.encrypted", settings.orbID)); err != nil {
			return err
		}

		if err := runCommand(settings, orbctl.strPtr(), nil, nil, newOrbctl(takeoffCtx), "--gitops", "writesecret", fmt.Sprintf("orbiter.%s.kubeconfig.encrypted", settings.orbID), "--value", "dummy"); err != nil {
			return err
		}

		writeSecretCmd := newOrbctl(takeoffCtx)
		writeSecretCmd.Stdin = buf

		return runCommand(settings, orbctl.strPtr(), nil, nil, writeSecretCmd, "--gitops", "writesecret", fmt.Sprintf("orbiter.%s.kubeconfig.encrypted", settings.orbID), "--stdin")
	}
}
