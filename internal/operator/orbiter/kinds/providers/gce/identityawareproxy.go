package gce

import (
	"fmt"

	"google.golang.org/api/servicemanagement/v1"
)

func ensureIdentityAwareProxyAPIEnabled(c *svcConfig) error {

	return operateFunc(
		func() {
			c.monitor.Debug("Enabling Identity Aware Proxy API")
		},
		servicesOpCall(c.apiClient.Services.Enable("iap.googleapis.com", &servicemanagement.EnableServiceRequest{
			ConsumerId: fmt.Sprintf("project:%s", c.projectID),
		}).
			Context(c.ctx).
			Do),
		func() error {
			c.monitor.Debug("Identity Aware Proxy API ensured")
			return nil
		},
	)()
}
