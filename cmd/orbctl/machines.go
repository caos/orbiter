package main

import (
	"fmt"

	orbcfg "github.com/caos/orbos/pkg/orb"

	"github.com/caos/orbos/pkg/labels"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/tree"
)

func machines(monitor mntr.Monitor, gitClient *git.Client, orbConfig *orbcfg.Orb, do func(machineIDs []string, machines map[string]infra.Machine, desired *tree.Tree) error) error {

	if err := orbConfig.IsConnectable(); err != nil {
		return err
	}

	if err := gitClient.Configure(orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
		return err
	}

	if err := gitClient.Clone(); err != nil {
		return err
	}

	if !gitClient.Exists(git.OrbiterFile) {
		return fmt.Errorf("%s not found", git.OrbiterFile)
	}

	monitor.Debug("Reading machines from orbiter.yml")

	desired, err := gitClient.ReadTree(git.OrbiterFile)
	if err != nil {
		return err
	}

	listMachines := orb.ListMachines(labels.NoopOperator("ORBOS"))

	orbID, err := orbConfig.ID()
	if err != nil {
		return err
	}

	machineIDs, machines, err := listMachines(
		monitor,
		desired,
		orbID,
	)

	return do(machineIDs, machines, desired)
}
