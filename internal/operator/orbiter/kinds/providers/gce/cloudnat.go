package gce

import (
	"github.com/caos/orbos/internal/helpers"
	uuid "github.com/satori/go.uuid"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

func destroyNetwork(c *svcConfig, deleteFirewalls []func() error) error {
	monitor := c.monitor.WithField("id", c.networkName)

	if err := helpers.Fanout(append(deleteFirewalls, func() error {
		return pruneErr(operateFunc(
			func() { monitor.Debug("Deleting Cloud NAT Router") },
			computeOpCall(c.computeClient.Routers.
				Delete(c.projectID, c.desired.Region, c.networkName).
				Context(c.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			func() error { monitor.Info("Cloud NAT Router deleted"); return nil },
		)(), 404)
	}))(); err != nil {
		return err
	}

	return pruneErr(operateFunc(
		func() { monitor.Debug("Deleting virtual private cloud network") },
		computeOpCall(c.computeClient.Networks.
			Delete(c.projectID, c.networkName).
			Context(c.ctx).
			RequestId(uuid.NewV1().String()).
			Do),
		func() error { monitor.Info("Virtual private cloud network deleted"); return nil },
	)(), 404)
}

func ensureNetwork(c *svcConfig, createFirewalls []func() error, deleteFirewalls []func() error) error {

	monitor := c.monitor.WithField("id", c.networkName)

	if err := pruneErr(operateFunc(
		func() { monitor.Debug("Creating virtual private cloud network") },
		computeOpCall(c.computeClient.Networks.
			Insert(c.projectID, &compute.Network{
				Name:                  c.networkName,
				AutoCreateSubnetworks: true,
			}).
			Context(c.ctx).
			RequestId(uuid.NewV1().String()).
			Do),
		func() error { monitor.Info("Virtual private cloud created"); return nil },
	)(), 409); err != nil {
		return err
	}

	return helpers.Fanout(append(createFirewalls, append(deleteFirewalls, func() error {
		return pruneErr(operateFunc(
			func() { monitor.Debug("Creating Cloud NAT Router") },
			computeOpCall(c.computeClient.Routers.
				Insert(c.projectID, c.desired.Region, &compute.Router{
					Name:    c.networkName,
					Network: c.networkURL,
					Nats: []*compute.RouterNat{{
						Name:                          c.networkName,
						NatIpAllocateOption:           "AUTO_ONLY",
						SourceSubnetworkIpRangesToNat: "ALL_SUBNETWORKS_ALL_IP_RANGES",
					}},
				}).
				Context(c.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			func() error { monitor.Info("Cloud NAT Router created"); return nil },
		)(), 409)
	})...))()
}

func pruneErr(err error, okCode int) error {
	e, ok := err.(*googleapi.Error)
	if !ok || e.Code != okCode {
		return err
	}
	return nil
}
