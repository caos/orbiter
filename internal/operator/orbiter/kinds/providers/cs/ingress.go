package cs

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"

	"github.com/caos/orbos/mntr"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
	dynamiclbmodel "github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
)

func desiredToCurrentVIP(current *Current) func(vip *dynamiclbmodel.VIP) string {
	return func(vip *dynamic.VIP) string {
		for idx := range vip.Transport {
			transport := vip.Transport[idx]
			address, ok := current.Current.Ingresses[transport.Name]
			if ok {
				return address.Location
			}
		}
		panic(fmt.Errorf("external address for %v is not ensured", vip))
	}
}

func checkAuth(_ infra.Machine) (string, int) {
	return `#!/bin/sh

set -e

curl -f -H "Authorization: Bearer $(cat /var/orbiter/cstoken)" "https://api.cloudscale.ch/v1/floating-ips"
`, 0
}

func notifyMaster(hostPools map[string][]*dynamiclbmodel.VIP, current *Current, poolsWithUnassignedVIPs map[string]bool) func(m infra.Machine) (string, bool) {
	return func(m infra.Machine) (string, bool) {

		machine := m.(*machine)
		return fmt.Sprintf(`#!/bin/sh

set -e

# API token (with write permission) to access the cloudscale.ch API.
api_token="$(cat /var/orbiter/cstoken)"

# Set of Floating IPs shared between the servers within the same VRRP group.
floating_ipv4='%s'

# UUID of the server that this script is running on.
# The UUID of the server can be retrieved using the API.
server_uuid='%s'

# Call the cloudscale.ch API to assign a specific Floating IP to this server.
set_master() {
	curl \
		-f \
		-H "Authorization: Bearer $api_token" \
		-F server="$server_uuid" \
		"https://api.cloudscale.ch/v1/floating-ips/$1"
}

for VIP in "$floating_ipv4"; do
		set_master $VIP
done
`, strings.Join(hostedVIPs(hostPools, m, current), " "), machine.server.UUID), poolsWithUnassignedVIPs[machine.poolName]
	}
}

func allHostedVIPs(hostPools map[string][]*dynamiclbmodel.VIP, service core.MachinesService, current *Current) (map[string][]string, error) {
	vips := make(map[string][]string, 0)
	pools, err := service.ListPools()
	if err != nil {
		return nil, err
	}
	for _, pool := range pools {
		poolMachines, err := service.List(pool)
		if err != nil {
			return nil, err
		}
		for idx := range poolMachines {
			machine := poolMachines[idx]
			vips[machine.ID()] = hostedVIPs(hostPools, machine, current)
		}
	}
	return vips, nil
}

func hostedVIPs(hostPools map[string][]*dynamiclbmodel.VIP, m infra.Machine, current *Current) []string {
	seen := make(map[string]bool)
	var locations []string
	for hostPool, vips := range hostPools {
		if m.(*machine).server.Tags["pool"] != hostPool {
			continue
		}
		for vipIdx := range vips {
			vip := vips[vipIdx]
			for transpIdx := range vip.Transport {
				transp := vip.Transport[transpIdx]
				addr, ok := current.Current.Ingresses[transp.Name]
				if !ok || addr == nil {
					continue
				}
				if _, ok := seen[addr.Location]; !ok {
					seen[addr.Location] = true
					locations = append(locations, addr.Location)
				}
			}
		}
	}
	return locations
}

func ensureTokens(monitor mntr.Monitor, token []byte, authCheckResult []dynamiclbmodel.AuthCheckResult) []func() error {
	var ensure []func() error
	for idx := range authCheckResult {
		ensure = append(ensure, func(result dynamiclbmodel.AuthCheckResult) func() error {
			return func() error { return ensureToken(monitor, token, result) }
		}(authCheckResult[idx]))
	}
	return ensure
}

func ensureToken(monitor mntr.Monitor, token []byte, authCheckResult dynamiclbmodel.AuthCheckResult) error {
	if authCheckResult.ExitCode != 0 {
		if err := authCheckResult.Machine.WriteFile("/var/orbiter/cstoken", bytes.NewReader(token), 600); err != nil {
			return err
		}
		monitor.WithField("machine", authCheckResult.Machine.ID()).Info("API token written")
	}
	return nil
}
