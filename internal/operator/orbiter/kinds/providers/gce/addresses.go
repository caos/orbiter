package gce

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
)

func queryAddresses(context *context, loadbalancing []*normalizedLoadbalancer) ([]func() error, error) {

	addresses := normalizedLoadbalancing(loadbalancing).uniqueAddresses()

	gceAddresses, err := context.client.Addresses.
		List(context.projectID, context.region).
		Filter(fmt.Sprintf(`description : "orb=%s;provider=%s*"`, context.orbID, context.providerID)).
		Fields("items(address,name,description)").
		Do()
	if err != nil {
		return nil, err
	}

	var operations []func() error

createLoop:
	for _, addr := range addresses {
		for _, gceAddress := range gceAddresses.Items {
			if gceAddress.Description == addr.gce.Description {
				addr.gce.Address = gceAddress.Address
				continue createLoop
			}
		}

		addr.gce.Name = newName()
		operations = append(operations, operateFunc(
			addr.log("Creating external address", true),
			context.client.Addresses.
				Insert(context.projectID, context.region, addr.gce).
				RequestId(uuid.NewV1().String()).
				Do,
			func(a *address) func() error {
				return func() error {
					newAddr, newAddrErr := context.client.Addresses.Get(context.projectID, context.region, a.gce.Name).
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

removeLoop:
	for _, gceAddress := range gceAddresses.Items {
		for _, address := range addresses {
			if gceAddress.Description == address.gce.Description {
				continue removeLoop
			}
		}
		operations = append(operations, removeResourceFunc(context.monitor, "external address", gceAddress.Name, context.client.Addresses.
			Delete(context.projectID, context.region, gceAddress.Name).
			RequestId(uuid.NewV1().String()).
			Do))
	}
	return operations, nil
}