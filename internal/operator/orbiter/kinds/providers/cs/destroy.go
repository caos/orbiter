package cs

import "github.com/caos/orbos/internal/helpers"

func destroy(context *context, current *Current) error {

	_, delFuncs, _, err := queryFloatingIPs(context, nil, current)

	pools, err := context.machinesService.ListPools()
	if err != nil {
		return err
	}
	for idx := range pools {
		pool := pools[idx]
		machines, err := context.machinesService.List(pool)
		if err != nil {
			return err
		}
		for idx := range machines {
			machine := machines[idx]
			delFuncs = append(delFuncs, func() error {
				remove, err := machine.Destroy()
				if err != nil {
					return err
				}
				return remove()
			})
		}
	}
	return helpers.Fanout(delFuncs)()
}
