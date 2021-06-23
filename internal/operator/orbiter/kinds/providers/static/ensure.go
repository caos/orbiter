package static

import (
	"sync"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic/wrap"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	dynamiclbmodel "github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/caos/orbos/mntr"
)

func query(
	desired *DesiredV0,
	current *Current,

	nodeAgentsDesired *common.DesiredNodeAgents,
	nodeAgentsCurrent *common.CurrentNodeAgents,
	lb interface{},

	monitor mntr.Monitor,
	internalMachinesService *machinesService,
	naFuncs core.IterateNodeAgentFuncs,
	orbiterCommit string,
) (ensureFunc orbiter.EnsureFunc, err error) {

	// TODO: Allow Changes
	desireHostnameFunc := desireHostname(desired.Spec.Pools, nodeAgentsDesired, nodeAgentsCurrent, monitor)

	queryNA, installNA := naFuncs(nodeAgentsCurrent)

	ensureNodeFunc := func(machine infra.Machine, pool string) error {

		na, _ := nodeAgentsDesired.Get(machine.ID())
		na.LeaveOSRepositories = desired.Spec.LeaveOSRepositories

		running, err := queryNA(machine, orbiterCommit)
		if err != nil {
			return err
		}
		if !running {
			if err := installNA(machine); err != nil {
				return err
			}
		}
		_, err = desireHostnameFunc(machine, pool)
		return err
	}
	internalMachinesService.onCreate = ensureNodeFunc

	var externalMachinesService core.MachinesService = internalMachinesService

	pools, err := internalMachinesService.ListPools()
	if err != nil {
		return nil, err
	}

	current.Current.Ingresses = make(map[string]*infra.Address)
	ensureLBFunc := func() *orbiter.EnsureResult {
		return &orbiter.EnsureResult{
			Err:  nil,
			Done: true,
		}
	}
	switch lbCurrent := lb.(type) {
	case *dynamiclbmodel.Current:

		mapVIP := func(vip *dynamiclbmodel.VIP) string {
			return vip.IP
		}

		wrappedMachinesService := wrap.MachinesService(internalMachinesService, *lbCurrent, &dynamiclbmodel.VRRP{
			VRRPInterface: "eth0",
			NotifyMaster:  nil,
			AuthCheck:     nil,
		}, mapVIP)
		externalMachinesService = wrappedMachinesService
		ensureLBFunc = func() *orbiter.EnsureResult {
			return orbiter.ToEnsureResult(wrappedMachinesService.InitializeDesiredNodeAgents())
		}
		deployPools, _, err := lbCurrent.Current.Spec(internalMachinesService)
		if err != nil {
			return nil, err
		}
		for _, pool := range deployPools {
			for _, vip := range pool {
				for _, src := range vip.Transport {
					current.Current.Ingresses[src.Name] = &infra.Address{
						Location:     vip.IP,
						FrontendPort: uint16(src.FrontendPort),
						BackendPort:  uint16(src.BackendPort),
					}
				}
			}
		}

		//	case *externallbmodel.Current:
		//		for name, address := range lbCurrent.Current.Addresses {
		//			current.Current.Ingresses[name] = address
		//		}
	default:
		return nil, errors.Errorf("Unknown load balancer of type %T", lb)
	}

	return func(pdf func(mntr.Monitor) error) *orbiter.EnsureResult {
		var wg sync.WaitGroup
		for _, pool := range pools {
			machines, listErr := internalMachinesService.List(pool)
			if listErr != nil {
				err = helpers.Concat(err, listErr)
			}
			for _, machine := range machines {
				wg.Add(1)
				go func(m infra.Machine, p string) {
					err = helpers.Concat(err, ensureNodeFunc(m, p))
					wg.Done()
				}(machine, pool)
			}
		}

		wg.Wait()
		if err != nil {
			return orbiter.ToEnsureResult(false, err)
		}
		result := ensureLBFunc()

		if result.Err == nil {
			fwDone, err := core.DesireInternalOSFirewall(monitor, nodeAgentsDesired, nodeAgentsCurrent, externalMachinesService, false, desired.Spec.ExternalInterfaces)
			result.Err = err
			result.Done = result.Done && fwDone
		}

		return result
	}, addPools(current, desired, externalMachinesService)
}
