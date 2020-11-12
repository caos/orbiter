package kubernetes

import (
	"fmt"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/mntr"
)

func firewallFunc(monitor mntr.Monitor, desired DesiredV0) (desire func(machine *initializedMachine)) {

	return func(machine *initializedMachine) {

		monitor = monitor.WithField("machine", machine.infra.ID())

		fw := map[string]*common.Allowed{
			"kubelet": {
				Port:     fmt.Sprintf("%d", 10250),
				Protocol: "tcp",
			},
		}

		if machine.pool.tier == Controlplane {
			fw["etcd"] = &common.Allowed{
				Port:     fmt.Sprintf("%d-%d", 2379, 2381),
				Protocol: "tcp",
			}
			fw["kube-scheduler"] = &common.Allowed{
				Port:     fmt.Sprintf("%d", 10251),
				Protocol: "tcp",
			}
			fw["kube-controller"] = &common.Allowed{
				Port:     fmt.Sprintf("%d", 10252),
				Protocol: "tcp",
			}
		}

		if desired.Spec.Networking.Network == "calico" {
			fw["calico-bgp"] = &common.Allowed{
				Port:     fmt.Sprintf("%d", 179),
				Protocol: "tcp",
			}
		}

		if desired.Spec.Networking.Network == "flannel" {
			fw["flannel-net"] = &common.Allowed{
				Port:     fmt.Sprintf("%d", 8285),
				Protocol: "udp",
			}
			fw["flannel-net2"] = &common.Allowed{
				Port:     fmt.Sprintf("%d", 8472),
				Protocol: "udp",
			}
		}
		for idx, value := range desired.Spec.Networking.OpenFirewallPorts {
			fw[fmt.Sprintf("custom-%d", idx)] = value
		}
		firewall := common.ToFirewall(fw)
		if firewall.IsContainedIn(machine.currentNodeagent.Open) && machine.desiredNodeagent.Firewall.Contains(firewall) {
			machine.currentMachine.FirewallIsReady = true
			monitor.Debug("firewall is ready")
			return
		}

		machine.currentMachine.FirewallIsReady = false
		machine.desiredNodeagent.Firewall.Merge(firewall)
		monitor.Info("firewall desired")
	}
}
