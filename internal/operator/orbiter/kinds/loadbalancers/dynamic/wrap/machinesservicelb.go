package wrap

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
)

var _ core.MachinesService = (*CmpSvcLB)(nil)

type CmpSvcLB struct {
	core.MachinesService
	dynamic dynamic.Current
	vrrp    *dynamic.VRRP
	vip     func(*dynamic.VIP) string
}

func MachinesService(svc core.MachinesService, curr dynamic.Current, vrrp *dynamic.VRRP, vip func(*dynamic.VIP) string) *CmpSvcLB {
	return &CmpSvcLB{
		MachinesService: svc,
		dynamic:         curr,
		vrrp:            vrrp,
		vip:             vip,
	}
}

func (i *CmpSvcLB) InitializeDesiredNodeAgents() (bool, error) {
	pools, err := i.ListPools()
	if err != nil {
		return false, err
	}

	done := true
	for _, pool := range pools {
		poolDone, err := i.desire(pool)
		if !poolDone {
			done = false
		}
		if err != nil {
			return done, err
		}
	}
	return done, nil
}

func (i *CmpSvcLB) Create(poolName string, desiredInstances int) (infra.Machines, error) {
	cmp, err := i.MachinesService.Create(poolName, desiredInstances)
	if err != nil {
		return nil, err
	}

	_, err = i.desire(poolName)
	machines := make([]infra.Machine, 0)

	for _, infraMachine := range cmp {
		machines = append(machines, machine(infraMachine, func() error {
			_, err := i.desire(poolName)
			return err
		}))
	}
	return machines, err
}

func (c *CmpSvcLB) desire(selfPool string) (bool, error) {
	return c.dynamic.Current.Desire(selfPool, c, c.vrrp, c.vip)
}
