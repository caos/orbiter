package gce

import (
	"strings"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	uuid "github.com/satori/go.uuid"
	"google.golang.org/api/compute/v1"
)

func destroy(svc *machinesService, delegates map[string]interface{}) error {
	return helpers.Fanout([]func() error{
		func() error {
			destroyLB, err := queryLB(svc.context, nil)
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
				for idx := range machines {
					delFuncs = append(delFuncs, machines[idx].Remove)
				}
			}
			if err := helpers.Fanout(delFuncs)(); err != nil {
				return err
			}

			return helpers.Fanout([]func() error{
				func() error {
					var deleteDisks []func() error

					deleteMonitor := svc.context.monitor.WithField("type", "persistent disk")
					currentVolumesList, err := svc.context.client.Disks.AggregatedList(svc.context.projectID).Do()
					if err != nil {
						return err
					}
					disks := make([]*compute.Disk, 0)
					region, err := svc.context.client.Regions.Get(svc.context.projectID, svc.context.desired.Region).Do()
					if err != nil {
						return err
					}
					diskList, ok := currentVolumesList.Items["regions/"+svc.context.desired.Region]
					if ok {
						for _, disk := range diskList.Disks {
							disks = append(disks, disk)
						}
					}
					for zoneURLI := range region.Zones {
						zoneURL := region.Zones[zoneURLI]
						zoneURLParts := strings.Split(zoneURL, "/")
						zone := zoneURLParts[(len(zoneURLParts) - 1)]
						diskList, ok := currentVolumesList.Items["zones/"+zone]
						if ok {
							for _, disk := range diskList.Disks {
								disks = append(disks, disk)
							}
						}
					}

					for _, delegate := range delegates {
						volumes, ok := delegate.([]infra.Volume)
						if ok {
							for idx := range volumes {
								diskName := volumes[idx].Name
								found := false
								zone := ""
								for _, currentVolume := range diskList.Disks {
									if currentVolume.Name == diskName {
										found = true
										zoneURLParts := strings.Split(currentVolume.Zone, "/")
										zone = zoneURLParts[(len(zoneURLParts) - 1)]
										break
									}
								}
								if found {
									deleteDisks = append(deleteDisks, deleteDiskFunc(svc.context, deleteMonitor.WithField("id", diskName), zone, diskName))
								}
							}
						}
					}
					return helpers.Fanout(deleteDisks)()
				},
				func() error {
					_, deleteFirewalls, err := queryFirewall(svc.context, nil)
					if err != nil {
						return err
					}
					return destroyNetwork(svc.context, deleteFirewalls)
				},
			})()
		},
	})()
}

func deleteDiskFunc(context *context, monitor mntr.Monitor, zone, id string) func() error {
	return func() error {
		return operateFunc(
			func() { monitor.Debug("Removing resource") },
			computeOpCall(context.client.Disks.Delete(context.projectID, zone, id).RequestId(uuid.NewV1().String()).Do),
			func() error { monitor.Info("Resource removed"); return nil },
		)()
	}
}
