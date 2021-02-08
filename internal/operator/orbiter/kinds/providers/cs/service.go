package cs

import (
	"context"
	"net/http"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk"

	"github.com/caos/orbos/mntr"
)

type svcConfig struct {
	ctx         context.Context
	monitor     mntr.Monitor
	networkName string
	orbID       string
	providerID  string
	desired     *Spec
	client      *cloudscale.Client
}

func service(ctx context.Context, monitor mntr.Monitor, desired *Spec, orbID, providerID string, oneoff bool) *machinesService {

	client := cloudscale.NewClient(&http.Client{
		Timeout: 30 * time.Second,
	})

	client.AuthToken = desired.APIToken.Value

	return newMachinesService(&svcConfig{
		ctx:        ctx,
		monitor:    monitor,
		orbID:      orbID,
		providerID: providerID,
		desired:    desired,
		client:     client,
	}, oneoff)
}
