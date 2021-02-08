package cs

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	"github.com/cloudscale-ch/cloudscale-go-sdk"
)

func queryServers(svc *machinesService, current *Current, loadbalancing map[string][]*dynamic.VIP, ensureNodeAgent func(m infra.Machine) error) ([]func() error, error) {

	pools, err := svc.machines()
	if err != nil {
		return nil, err
	}

	var ensureServers []func() error
	for poolName, machines := range pools {
		for idx := range machines {
			mach := machines[idx]
			ensureServers = append(ensureServers, func(poolName string, m *machine) func() error {
				return func() error {
					return ensureServer(svc, current, loadbalancing, poolName, m, ensureNodeAgent)
				}
			}(poolName, mach))
		}
	}
	return ensureServers, nil
}

func ensureServer(svc *machinesService, current *Current, loadbalancing map[string][]*dynamic.VIP, poolName string, machine *machine, ensureNodeAgent func(m infra.Machine) error) (err error) {
	defer func() {
		if err == nil {
			err = ensureOS(ensureNodeAgent, machine, loadbalancing, current, svc.cfg)
		}
	}()

	_, isExternal := loadbalancing[poolName]
	if svc.oneoff {
		isExternal = true
	}
	// Always use external ips
	isExternal = true
	hasPublicInterface := false
	var privateInterfaces []cloudscale.Interface
	for idx := range machine.server.Interfaces {
		interf := machine.server.Interfaces[idx]
		if interf.Type == "public" {
			hasPublicInterface = true
		} else {
			privateInterfaces = append(privateInterfaces, interf)
		}
	}

	var updateInterfaces []cloudscale.InterfaceRequest
	if isExternal && !hasPublicInterface {
		updateInterfaces = append(interfaces(machine.server.Interfaces).toRequests(), cloudscale.InterfaceRequest{Network: "public"})
	}

	if !isExternal && hasPublicInterface {
		updateInterfaces = interfaces(privateInterfaces).toRequests()
	}

	if updateInterfaces == nil {
		return nil
	}
	return updateServer(svc.cfg, machine.server, &updateInterfaces)
}

func ensureOS(ensureNodeAgent func(m infra.Machine) error, machine *machine, loadbalancing map[string][]*dynamic.VIP, current *Current, context *svcConfig) error {
	if err := ensureNodeAgent(machine); err != nil {
		return err
	}

	return nil
}

func updateServer(cfg *svcConfig, server *cloudscale.Server, interfaces *[]cloudscale.InterfaceRequest) error {
	return cfg.client.Servers.Update(cfg.ctx, server.UUID, &cloudscale.ServerUpdateRequest{
		TaggedResourceRequest: cloudscale.TaggedResourceRequest{Tags: server.Tags},
		Name:                  server.Name,
		Status:                server.Status,
		Flavor:                server.Flavor.Name,
		Interfaces:            interfaces,
	})
}

type interfaces []cloudscale.Interface

func (i interfaces) toRequests() []cloudscale.InterfaceRequest {
	var requests []cloudscale.InterfaceRequest
	for idx := range i {
		interf := i[idx]
		addr := addresses(interf.Addresses).toRequest()
		requests = append(requests, cloudscale.InterfaceRequest{
			Network:   interf.Network.UUID,
			Addresses: &addr,
		})
	}
	return requests
}

type addresses []cloudscale.Address

func (a addresses) toRequest() []cloudscale.AddressRequest {
	var requests []cloudscale.AddressRequest
	for idx := range a {
		addr := a[idx]
		requests = append(requests, cloudscale.AddressRequest{
			Subnet:  addr.Subnet.UUID,
			Address: addr.Address,
		})
	}
	return requests
}
