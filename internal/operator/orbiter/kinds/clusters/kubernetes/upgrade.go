package kubernetes

import (
	"fmt"

	"github.com/caos/orbos/pkg/kubernetes"

	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/mntr"
)

type initializedMachines []*initializedMachine

func (c initializedMachines) Len() int           { return len(c) }
func (c initializedMachines) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c initializedMachines) Less(i, j int) bool { return c[i].infra.ID() < c[j].infra.ID() }

func ensureSoftware(
	monitor mntr.Monitor,
	target KubernetesVersion,
	k8sClient *kubernetes.Client,
	controlplane []*initializedMachine,
	workers []*initializedMachine) (bool, error) {

	sortedMachines := append(controlplane, workers...)
	from, to, err := findPath(monitor, sortedMachines, target)
	if err != nil {
		return false, err
	}

	monitor.WithFields(map[string]interface{}{
		"currentSoftware":   from,
		"currentKubernetes": from.Kubelet,
		"desiredSofware":    to,
		"desiredKubernetes": to.Kubelet,
	}).Debug("Ensuring kubernetes version")

	return step(k8sClient, monitor, sortedMachines, from, to)
}

func findPath(
	monitor mntr.Monitor,
	machines []*initializedMachine,
	target KubernetesVersion,
) (common.Software, common.Software, error) {

	var overallLowKubelet KubernetesVersion
	var overallLowKubeletMinor int
	zeroSW := common.Software{}

	for _, machine := range machines {
		id := machine.infra.ID()
		if machine.node == nil {
			continue
		}

		nodeinfoKubelet := machine.node.Status.NodeInfo.KubeletVersion

		monitor.WithFields(map[string]interface{}{
			"machine": id,
			"kubelet": nodeinfoKubelet,
		}).Debug("Found kubelet version from node info")
		kubelet := ParseString(nodeinfoKubelet)
		if kubelet == Unknown {
			return zeroSW, zeroSW, errors.Errorf("parsing version %s from nodes %s info failed", nodeinfoKubelet, id)
		}

		kubeletMinor, err := kubelet.ExtractMinor(monitor)
		if err != nil {
			return zeroSW, zeroSW, errors.Wrapf(err, "extracting minor from kubelet version %s from nodes %s info failed", nodeinfoKubelet, id)
		}

		if overallLowKubelet == Unknown {
			overallLowKubelet = kubelet
			overallLowKubeletMinor = kubeletMinor
			continue
		}

		kubeletPatch, err := kubelet.ExtractPatch(monitor)
		if err != nil {
			return zeroSW, zeroSW, errors.Wrapf(err, "extracting patch from kubelet version %s from nodes %s info failed", nodeinfoKubelet, id)
		}
		tmpOverallLowKubeletMinor, err := overallLowKubelet.ExtractMinor(monitor)
		if err != nil {
			return zeroSW, zeroSW, errors.Wrapf(err, "extracting minor from overall kubelet version %s failed", overallLowKubelet)
		}
		tmpOverallLowKubeletPatch, err := overallLowKubelet.ExtractPatch(monitor)
		if err != nil {
			return zeroSW, zeroSW, errors.Wrapf(err, "extracting patch from overall kubelet version %s failed", overallLowKubelet)
		}

		if kubeletMinor < tmpOverallLowKubeletMinor ||
			kubeletMinor == tmpOverallLowKubeletMinor && kubeletPatch < tmpOverallLowKubeletPatch {
			overallLowKubelet = kubelet
			overallLowKubeletMinor = kubeletMinor
		}
	}

	if overallLowKubelet == target || overallLowKubelet == Unknown {
		target := target.DefineSoftware()
		monitor.WithFields(map[string]interface{}{
			"from": overallLowKubelet,
			"to":   target,
		}).Debug("Cluster is up to date")
		return target, target, nil
	}

	targetMinor, err := target.ExtractMinor(monitor)
	if err != nil {
		return zeroSW, zeroSW, errors.Wrapf(err, "extracting minor from target version %s failed", target)
	}

	if targetMinor < overallLowKubeletMinor {
		return zeroSW, zeroSW, errors.Errorf("downgrading from %s to %s is not possible as they are on different minors", overallLowKubelet, target)
	}

	overallLowKubeletSoftware := overallLowKubelet.DefineSoftware()
	if (targetMinor - overallLowKubeletMinor) < 2 {
		monitor.WithFields(map[string]interface{}{
			"from":                   overallLowKubelet,
			"to":                     target,
			"targetMinor":            targetMinor,
			"overallLowKubeletMinor": overallLowKubeletMinor,
		}).Debug("Desired version can be reached directly")
		return overallLowKubeletSoftware, target.DefineSoftware(), nil
	}

	nextHighestMinor := overallLowKubelet.NextHighestMinor()
	monitor.WithFields(map[string]interface{}{
		"from":         overallLowKubelet,
		"fromMinor":    overallLowKubeletMinor,
		"intermediate": nextHighestMinor,
		"to":           target,
		"toMinor":      targetMinor,
	}).Debug("Desired version can be reached via an intermediate version")
	return overallLowKubeletSoftware, nextHighestMinor.DefineSoftware(), nil
}

func step(
	k8sClient *kubernetes.Client,
	monitor mntr.Monitor,
	sortedMachines initializedMachines,
	from common.Software,
	to common.Software,
) (bool, error) {

	for _, machine := range sortedMachines {
		if machine.node != nil && machine.node.Labels["orbos.ch/updating"] == machine.node.Status.NodeInfo.KubeletVersion {
			delete(machine.node.Labels, "orbos.ch/updating")
			if k8sClient.Tainted(machine.node, kubernetes.Updating) {
				machine.node.Spec.Taints = k8sClient.RemoveFromTaints(machine.node.Spec.Taints, kubernetes.Updating)
			}
			if err := k8sClient.UpdateNode(machine.node); err != nil {
				return false, err
			}
		}
	}
	for idx, machine := range sortedMachines {

		next, err := plan(k8sClient, monitor, machine, idx == 0, from, to)
		if err != nil {
			return false, errors.Wrapf(err, "planning machine %s failed", machine.infra.ID())
		}

		if next == nil {
			continue
		}
		return false, next()
	}
	return true, nil
}

func plan(
	k8sClient *kubernetes.Client,
	monitor mntr.Monitor,
	machine *initializedMachine,
	isFirstControlplane bool,
	from common.Software,
	to common.Software,
) (func() error, error) {
	from.Kubeadm = common.Package{}

	isControlplane := machine.pool.tier == Controlplane

	id := machine.infra.ID()
	machinemonitor := monitor.WithField("machine", id)

	awaitNodeAgent := func() error {
		machinemonitor.Info("Awaiting node agent")
		return nil
	}

	drain := func() error {
		if isControlplane || machine.node == nil || machine.node.Spec.Unschedulable {
			return nil
		}
		machine.node.Labels["orbos.ch/updating"] = to.Kubelet.Version
		return k8sClient.Drain(machine.currentMachine, machine.node, kubernetes.Updating, false)
	}

	ensureSoftware := func(packages common.Software, phase string) func() error {
		swmonitor := machinemonitor.WithField("phase", phase)
		zeroPkg := common.Package{}
		return func() error {
			if !packages.Kubelet.Equals(zeroPkg) &&
				!machine.currentNodeagent.Software.Kubelet.Equals(packages.Kubelet) ||
				!packages.Containerruntime.Equals(zeroPkg) &&
					!machine.currentNodeagent.Software.Containerruntime.Equals(packages.Containerruntime) {
				if err := drain(); err != nil {
					return err
				}
			}
			if !softwareContains(*machine.desiredNodeagent.Software, packages) {
				swmonitor.Changed("Kubernetes software desired")
			} else {
				swmonitor.Info("Awaiting kubernetes software")
			}
			machine.desiredNodeagent.Software.Merge(packages)
			return nil
		}
	}

	labelUpgradeState := func() error {
		machine.node.Labels["orbos.ch/kubeadm-upgraded"] = to.Kubelet.Version
		return k8sClient.UpdateNode(machine.node)
	}

	migrate := func() (err error) {

		defer func() {
			err = errors.Wrapf(err, "migrating node %s failed", machine.infra.ID())
		}()

		if err := drain(); err != nil {
			return err
		}

		upgradeAction := "node"
		if isFirstControlplane {
			machinemonitor.Info("Migrating first controlplane node")
			upgradeAction = fmt.Sprintf("apply %s --yes", to.Kubelet.Version)
		} else {
			machinemonitor.Info("Migrating node")
		}

		if _, err := machine.infra.Execute(nil, fmt.Sprintf("sudo kubeadm upgrade %s", upgradeAction)); err != nil {
			return err
		}

		return labelUpgradeState()
	}

	nodeIsReady := machine.currentNodeagent.NodeIsReady

	if !machine.currentMachine.Joined {
		if softwareContains(machine.currentNodeagent.Software, to) {
			if !nodeIsReady {
				return awaitNodeAgent, nil
			}

			// This node needs to be joined first
			return nil, nil
		}
		return ensureSoftware(to, "Prepare for joining"), nil
	}

	if !machine.currentNodeagent.Software.Kubeadm.Equals(to.Kubeadm) || !machine.desiredNodeagent.Software.Kubeadm.Equals(to.Kubeadm) {
		if !softwareContains(machine.currentNodeagent.Software, from) || !softwareContains(*machine.desiredNodeagent.Software, from) {
			return ensureSoftware(from, "Reconcile lower kubernetes software"), nil
		}

		return ensureSoftware(common.Software{Kubeadm: to.Kubeadm}, "Update kubeadm"), nil
	}

	kubadmUpgraded := machine.node.Labels["orbos.ch/kubeadm-upgraded"]
	if kubadmUpgraded != to.Kubelet.Version {

		if kubadmUpgraded == "" {
			return labelUpgradeState, nil
		}

		return migrate, nil
	}

	if !softwareContains(machine.currentNodeagent.Software, to) || !softwareContains(*machine.desiredNodeagent.Software, to) {
		return ensureSoftware(to, "Reconcile kubernetes software"), nil
	}

	if !nodeIsReady {
		return awaitNodeAgent, nil
	}

	return nil, nil
}
