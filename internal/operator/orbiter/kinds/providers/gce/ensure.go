package gce

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"

	"github.com/caos/orbos/internal/api"

	"github.com/caos/orbos/internal/helpers"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic/wrap"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	dynamiclbmodel "github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/orbiter"
	//	externallbmodel "github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/external"
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
	vips, _, err := lbCurrent.Current.Spec(svc)
	if err != nil {
		return nil, err
	}
	normalized, firewalls := normalize(svc.cfg, vips)

	var (
		ensureLB             func() error
		createFWs, deleteFWs []func() error
	)
	if err := helpers.Fanout([]func() error{
		func() error {
			var err error
			ensureLB, err = queryLB(svc.cfg, normalized)
			return err
		},
		func() error {
			var err error
			createFWs, deleteFWs, err = queryFirewall(svc.cfg, firewalls)
			return err
		},
	})(); err != nil {
		return nil, err
	}

	current.Current.Ingresses = make(map[string]*infra.Address)
	for _, lb := range normalized {
		current.Current.Ingresses[lb.transport] = &infra.Address{
			Location:     lb.address.gce.Address,
			BackendPort:  internalPort(lb),
			FrontendPort: externalPort(lb),
		}
	}

	queryNA, installNA := naFuncs(nodeAgentsCurrent)

	desireNodeAgent := func(pool string, machine infra.Machine) error {

		machineID := machine.ID()
		machineMonitor := svc.cfg.monitor.WithField("machine", machineID)
		na, _ := nodeAgentsDesired.Get(machineID)
		if na.Software.Health.Config == nil {
			na.Software.Health.Config = make(map[string]string)
		}

		for _, lb := range normalized {
			for _, destPool := range lb.targetPool.destPools {
				if pool == destPool {
					key := fmt.Sprintf(
						"%s:%d%s",
						"0.0.0.0",
						lb.healthcheck.gce.Port,
						lb.healthcheck.gce.RequestPath)

					value := fmt.Sprintf(
						"--protocol %s --ip %s --port %d --path %s --status %d --proxy=%t",
						lb.healthcheck.desired.Protocol,
						machine.IP(),
						internalPort(lb),
						lb.healthcheck.desired.Path,
						lb.healthcheck.desired.Code,
						lb.healthcheck.proxyProtocol,
					)

					if v := na.Software.Health.Config[key]; v != value {
						na.Software.Health.Config[key] = value
						machineMonitor.WithFields(map[string]interface{}{
							"listen": key,
							"checks": value,
						}).Debug("Healthcheck desired")
					}
					fw := common.ToFirewall("external", map[string]*common.Allowed{
						lb.healthcheck.gce.Description: {
							Port:     fmt.Sprintf("%d", lb.healthcheck.gce.Port),
							Protocol: "tcp",
						},
					})
					if !na.Firewall.Contains(fw) {
						machineMonitor.WithField("ports", fw.ToCurrent()).Debug("Firewall desired")
					}
					na.Firewall.Merge(fw)
				}
			}
		}
		running, err := queryNA(machine, orbiterCommit)
		if err != nil {
			return err
		}
		if !running {
			return installNA(machine)
		}

		return nil
	}

	svc.onCreate = desireNodeAgent
	wrappedMachines := wrap.MachinesService(svc, *lbCurrent, nil, func(vip *dynamic.VIP) string {
		for _, transport := range vip.Transport {
			address, ok := current.Current.Ingresses[transport.Name]
			if ok {
				return address.Location
			}
		}
		panic(fmt.Errorf("external address for %v is not ensured", vip))
	})
	return func(pdf api.PushDesiredFunc) *orbiter.EnsureResult {

		var done bool
		return orbiter.ToEnsureResult(done, helpers.Fanout([]func() error{
			func() error { return ensureIdentityAwareProxyAPIEnabled(svc.cfg) },
			func() error { return ensureNetwork(svc.cfg, createFWs, deleteFWs) },
			svc.restartPreemptibleMachines,
			ensureLB,
			func() error {
				pools, err := svc.ListPools()
				if err != nil {
					return err
				}

				var desireNodeAgents []func() error
				for _, pool := range pools {
					machines, listErr := svc.List(pool)
					if listErr != nil {
						err = helpers.Concat(err, listErr)
					}
					for _, machine := range machines {
						desireNodeAgents = append(desireNodeAgents, func(p string, m infra.Machine) func() error {
							return func() error {
								return desireNodeAgent(p, m)
							}
						}(pool, machine))
					}
				}
				return helpers.Fanout(desireNodeAgents)()
			},
			func() error {
				var err error
				lbDone, err := wrappedMachines.InitializeDesiredNodeAgents()
				if err != nil {
					return err
				}

				fwDone, err := core.DesireInternalOSFirewall(svc.cfg.monitor, nodeAgentsDesired, nodeAgentsCurrent, svc, false, []string{"eth0"})
				if err != nil {
					return err
				}
				done = lbDone && fwDone

				return err
			},
		})())
	}, initPools(current, desired, svc, normalized, wrappedMachines)
}

func internalPort(lb *normalizedLoadbalancer) uint16 {
	return lb.backendPort
}

func externalPort(lb *normalizedLoadbalancer) uint16 {
	port, err := strconv.Atoi(strings.Split(lb.forwardingRule.gce.PortRange, "-")[0])
	if err != nil {
		panic(err)
	}
	return uint16(port)
}
