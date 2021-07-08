package centos

import (
	"fmt"
	"strings"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/mntr"
)

func getAddAndRemovePorts(
	monitor mntr.Monitor,
	zone string,
	current *common.ZoneDesc,
	desiredPorts []*common.Allowed,
	open []string,
) (
	[]string,
	[]string,
	error,
) {

	ensure := make([]string, 0)
	remove := make([]string, 0)

	alreadyOpen, err := getPorts(monitor, zone)
	if err != nil {
		return nil, nil, err
	}
	alwaysOpen := ignoredPorts(open)

	//ports that should stay open
	for _, open := range alwaysOpen {
		found := false
		openStr := fmt.Sprintf("%s/%s", open.Port, open.Protocol)
		if alreadyOpen != nil && len(alreadyOpen) > 0 {
			for _, open := range alreadyOpen {
				if open == openStr {
					found = true
					break
				}
			}
		}
		if !found {
			ensure = append(ensure, fmt.Sprintf("--add-port=%s", openStr))
		}
	}

	//desired ports
	for _, desired := range desiredPorts {
		found := false
		desStr := fmt.Sprintf("%s/%s", desired.Port, desired.Protocol)
		if alreadyOpen != nil && len(alreadyOpen) > 0 {
			for _, open := range alreadyOpen {
				if open == desStr {
					found = true
					break
				}
			}
		}
		if !found {
			ensure = append(ensure, fmt.Sprintf("--add-port=%s", desStr))
		}
	}

	//port that are not desired anymore
	for _, open := range alreadyOpen {
		found := false

		fields := strings.Split(open, "/")
		port := fields[0]
		protocol := fields[1]

		current.FW = append(current.FW, &common.Allowed{
			Port:     port,
			Protocol: protocol,
		})

		if desiredPorts != nil && len(desiredPorts) > 0 {
			for _, desired := range desiredPorts {
				if desired.Port == port && desired.Protocol == protocol {
					found = true
					break
				}
			}
		}

		if !found {
			if alwaysOpen != nil && len(alwaysOpen) > 0 {
				for _, open := range alwaysOpen {
					if open.Port == port && open.Protocol == protocol {
						found = true
						break
					}
				}
			}
		}

		if !found {
			remove = append(remove, fmt.Sprintf("--remove-port=%s", open))
		}
	}

	return ensure, remove, nil
}

func getPorts(monitor mntr.Monitor, zone string) ([]string, error) {
	return listFirewall(monitor, zone, "--list-ports")
}

func ignoredPorts(ports []string) []*common.Allowed {
	allowed := make([]*common.Allowed, len(ports))
	for idx, port := range ports {
		allowed[idx] = &common.Allowed{
			Port:     port,
			Protocol: "tcp",
		}
	}
	return allowed
}
