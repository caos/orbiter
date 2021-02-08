package cs

import "github.com/caos/orbos/internal/helpers"

func destroy(svc *machinesService, current *Current) error {

	_, delFuncs, _, err := queryFloatingIPs(svc.cfg, nil, current)

	pools, err := svc.ListPools()
	if err != nil {
		return err
	}
	for idx := range pools {
		pool := pools[idx]
		machines, err := svc.List(pool)
		if err != nil {
			return err
		}
		for idx := range machines {
			machine := machines[idx]
			delFuncs = append(delFuncs, machine.Remove)
		}
	}
	return helpers.Fanout(delFuncs)()
}
