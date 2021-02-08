package gce

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
)

var _ ensureLBFunc = queryAddresses

func queryAddresses(cfg *svcConfig, loadbalancing []*normalizedLoadbalancer) ([]func() error, []func() error, error) {

	addresses := normalizedLoadbalancing(loadbalancing).uniqueAddresses()

	gceAddresses, err := cfg.computeClient.Addresses.
		List(cfg.projectID, cfg.desired.Region).
		Context(cfg.ctx).
		Filter(fmt.Sprintf(`description : "orb=%s;provider=%s*"`, cfg.orbID, cfg.providerID)).
		Fields("items(address,name,description)").
		Do()
	if err != nil {
		return nil, nil, err
	}

	var ensure []func() error

createLoop:
	for _, addr := range addresses {
		for _, gceAddress := range gceAddresses.Items {
			if gceAddress.Description == addr.gce.Description {
				addr.gce.Address = gceAddress.Address
				continue createLoop
			}
		}

		addr.gce.Name = newName()
		ensure = append(ensure, operateFunc(
			addr.log("Creating external address", true),
			computeOpCall(cfg.computeClient.Addresses.
				Insert(cfg.projectID, cfg.desired.Region, addr.gce).
				Context(cfg.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			func(a *address) func() error {
				return func() error {
					newAddr, newAddrErr := cfg.computeClient.Addresses.
						Get(cfg.projectID, cfg.desired.Region, a.gce.Name).
						Context(cfg.ctx).
						Fields("address").
						Do()
					if newAddrErr != nil {
						return newAddrErr
					}
					a.gce.Address = newAddr.Address
					a.log("External address created", false)()
					return nil
				}
			}(addr)))
	}

	var remove []func() error
removeLoop:
	for _, gceAddress := range gceAddresses.Items {
		for _, address := range addresses {
			if gceAddress.Description == address.gce.Description {
				continue removeLoop
			}
		}
		remove = append(remove, removeResourceFunc(cfg.monitor, "external address", gceAddress.Name, cfg.computeClient.Addresses.
			Delete(cfg.projectID, cfg.desired.Region, gceAddress.Name).
			Context(cfg.ctx).
			RequestId(uuid.NewV1().String()).
			Do))
	}
	return ensure, remove, nil
}
