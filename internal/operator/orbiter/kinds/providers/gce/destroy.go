package gce

import (
	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	uuid "github.com/satori/go.uuid"
)

func destroy(svc *machinesService, delegates map[string]interface{}) error {
	return helpers.Fanout([]func() error{
		func() error {
			destroyLB, err := queryLB(svc.cfg, nil)
			if err != nil {
				return err
			}
			return destroyLB()
		},
		func() error {
			pools, err := svc.ListPools()
			if err != nil {
				return err
			}
			var delFuncs []func() error
			for _, pool := range pools {
				machines, err := svc.List(pool)
				if err != nil {
					return err
				}
				for _, machine := range machines {
					delFuncs = append(delFuncs, machine.Remove)
				}
			}
			if err := helpers.Fanout(delFuncs)(); err != nil {
				return err
			}

			return helpers.Fanout([]func() error{
				func() error {
					var deleteDisks []func() error

					deleteMonitor := svc.cfg.monitor.WithField("type", "persistent disk")

					for kind, delegate := range delegates {
						volumes, ok := delegate.([]infra.Volume)
						if ok {
							for idx := range volumes {
								diskName := volumes[idx].Name
								deleteDisks = append(deleteDisks, deleteDiskFunc(svc.cfg, deleteMonitor.WithField("id", diskName), kind, diskName))
							}
						}
					}
					return helpers.Fanout(deleteDisks)()
				},
				func() error {
					_, deleteFirewalls, err := queryFirewall(svc.cfg, nil)
					if err != nil {
						return err
					}
					return destroyNetwork(svc.cfg, deleteFirewalls)
				},
			})()
		},
	})()
}

func deleteDiskFunc(cfg *svcConfig, monitor mntr.Monitor, kind, id string) func() error {
	return func() error {
		return operateFunc(
			func() { monitor.Debug("Removing resource") },
			computeOpCall(cfg.computeClient.Disks.Delete(cfg.projectID, cfg.desired.Zone, id).
				Context(cfg.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			func() error { monitor.Info("Resource removed"); return nil },
		)()
	}
}
