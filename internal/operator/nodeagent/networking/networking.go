package networking

import (
	"context"

	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/networking/centos"
	"github.com/caos/orbos/mntr"
)

func Ensurer(ctx context.Context, monitor mntr.Monitor, os dep.OperatingSystem) nodeagent.NetworkingEnsurer {
	switch os {
	case dep.CentOS:
		return centos.Ensurer(ctx, monitor)
	default:
		return noopEnsurer()
	}
}
