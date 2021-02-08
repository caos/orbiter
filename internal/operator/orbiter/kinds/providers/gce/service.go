package gce

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"google.golang.org/api/servicemanagement/v1"

	"github.com/caos/orbos/mntr"
	"github.com/pkg/errors"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

type svcConfig struct {
	monitor       mntr.Monitor
	networkName   string
	networkURL    string
	orbID         string
	providerID    string
	projectID     string
	desired       *Spec
	computeClient *compute.Service
	apiClient     *servicemanagement.APIService
	ctx           context.Context
	clientOptions *option.ClientOption
}

func service(ctx context.Context, monitor mntr.Monitor, desired *Spec, orbID, providerID string, oneoff bool) (*machinesService, error) {

	jsonKey := []byte(desired.JSONKey.Value)

	creds := option.WithCredentialsJSON(jsonKey)

	computeClient, err := compute.NewService(ctx, creds)
	if err != nil {
		return nil, err
	}

	apiClient, err := servicemanagement.NewService(ctx, creds)
	if err != nil {
		return nil, err
	}

	key := struct {
		ProjectID string `json:"project_id"`
	}{}
	if err := errors.Wrap(json.Unmarshal(jsonKey, &key), "extracting project id from jsonkey failed"); err != nil {
		return nil, err
	}

	monitor = monitor.WithField("projectID", key.ProjectID)
	h := fnv.New32()
	h.Write([]byte(orbID))
	networkName := fmt.Sprintf("orbos-network-%d", h.Sum32())
	h.Reset()
	networkURL := fmt.Sprintf("projects/%s/global/networks/%s", key.ProjectID, networkName)

	return newMachinesService(&svcConfig{
			monitor:       monitor,
			orbID:         orbID,
			providerID:    providerID,
			projectID:     key.ProjectID,
			desired:       desired,
			computeClient: computeClient,
			apiClient:     apiClient,
			ctx:           ctx,
			networkName:   networkName,
			networkURL:    networkURL,
		}, oneoff),
		nil
}
