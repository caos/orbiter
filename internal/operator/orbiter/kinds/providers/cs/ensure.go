package cs

import (
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/api"
	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	dynamiclbmodel "github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic/wrap"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
)

func query(
	desired *Spec,
	current *Current,
	lb interface{},
	svc *machinesService,
	nodeAgentsCurrent *common.CurrentNodeAgents,
	nodeAgentsDesired *common.DesiredNodeAgents,
	naFuncs core.IterateNodeAgentFuncs,
	orbiterCommit string,
) (ensureFunc orbiter.EnsureFunc, err error) {

	lbCurrent, ok := lb.(*dynamiclbmodel.Current)
	if !ok {
		panic(errors.Errorf("Unknown or unsupported load balancing of type %T", lb))
	}

	hostPools, authChecks, err := lbCurrent.Current.Spec(svc)
	if err != nil {
		return nil, err
	}

	ensureFIPs, removeFIPs, poolsWithUnassignedVIPs, err := queryFloatingIPs(svc.cfg, hostPools, current)
	if err != nil {
		return nil, err
	}

	queryNA, installNA := naFuncs(nodeAgentsCurrent)
	ensureNodeAgent := func(m infra.Machine) error {
		running, err := queryNA(m, orbiterCommit)
		if err != nil {
			return err
		}
		if !running {
			return installNA(m)
		}
		return nil
	}

	ensureServers, err := queryServers(svc, current, hostPools, ensureNodeAgent)
	if err != nil {
		return nil, err
	}

	svc.onCreate = func(pool string, m infra.Machine) error {
		_, err := core.DesireInternalOSFirewall(svc.cfg.monitor, nodeAgentsDesired, nodeAgentsCurrent, svc, true, []string{"eth0"})
		if err != nil {
			return err
		}

		vips := hostedVIPs(hostPools, m, current)
		_, err = core.DesireOSNetworkingForMachine(svc.cfg.monitor, nodeAgentsDesired, nodeAgentsCurrent, m, "dummy", vips)
		if err != nil {
			return err
		}

		return ensureServer(svc, current, hostPools, pool, m.(*machine), ensureNodeAgent)
	}
	wrappedMachines := wrap.MachinesService(svc, *lbCurrent, &dynamiclbmodel.VRRP{
		VRRPInterface: "eth1",
		NotifyMaster:  notifyMaster(hostPools, current, poolsWithUnassignedVIPs),
		AuthCheck:     checkAuth,
	}, desiredToCurrentVIP(current))
	return func(pdf api.PushDesiredFunc) *orbiter.EnsureResult {
		var done bool
		return orbiter.ToEnsureResult(done, helpers.Fanout([]func() error{
			func() error {
				return helpers.Fanout(ensureTokens(svc.cfg.monitor, []byte(desired.APIToken.Value), authChecks))()
			},
			func() error { return helpers.Fanout(ensureFIPs)() },
			func() error { return helpers.Fanout(removeFIPs)() },
			func() error { return helpers.Fanout(ensureServers)() },
			func() error {
				lbDone, err := wrappedMachines.InitializeDesiredNodeAgents()
				if err != nil {
					return err
				}

				fwDone, err := core.DesireInternalOSFirewall(svc.cfg.monitor, nodeAgentsDesired, nodeAgentsCurrent, svc, true, []string{"eth0"})
				if err != nil {
					return err
				}

				vips, err := allHostedVIPs(hostPools, svc, current)
				if err != nil {
					return err
				}
				nwDone, err := core.DesireOSNetworking(svc.cfg.monitor, nodeAgentsDesired, nodeAgentsCurrent, svc, "dummy", vips)
				if err != nil {
					return err
				}

				done = lbDone && fwDone && nwDone
				return nil
			},
		})())
	}, addPools(current, desired, wrappedMachines)
}
