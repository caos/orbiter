package main

import (
	"context"
	"errors"

	"github.com/caos/orbos/pkg/labels"

	"github.com/caos/orbos/internal/api"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/orb"
	cfg "github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/tree"
)

func machines(ctx context.Context, monitor mntr.Monitor, gitClient *git.Client, orbConfig *cfg.Orb, do func(machineIDs []string, machines map[string]infra.Machine, desired *tree.Tree) error) error {

	if err := orbConfig.IsConnectable(); err != nil {
		return err
	}

	if err := gitClient.Configure(ctx, orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
		return err
	}

	if err := gitClient.Clone(); err != nil {
		return err
	}

	foundOrbiter, err := api.ExistsOrbiterYml(gitClient)
	if err != nil {
		return err
	}

	if !foundOrbiter {
		return errors.New("Orbiter.yml not found")
	}

	monitor.Debug("Reading machines from orbiter.yml")

	desired, err := api.ReadOrbiterYml(gitClient)
	if err != nil {
		return err
	}

	listMachines := orb.ListMachines(ctx, labels.NoopOperator("ORBOS"))

	machineIDs, machines, err := listMachines(
		monitor,
		desired,
		orbConfig.URL,
	)

	return do(machineIDs, machines, desired)
}
