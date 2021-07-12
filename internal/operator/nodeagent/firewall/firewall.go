package firewall

import (
	"context"

	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/firewall/centos"
	"github.com/caos/orbos/mntr"
)

func Ensurer(ctx context.Context, monitor mntr.Monitor, os dep.OperatingSystem, open []string) nodeagent.FirewallEnsurer {
	switch os {
	case dep.CentOS:
		return centos.Ensurer(ctx, monitor, open)
	default:
		return noopEnsurer()
	}
}
