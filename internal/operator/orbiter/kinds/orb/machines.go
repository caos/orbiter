package orb

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/labels"
	"github.com/caos/orbos/pkg/tree"
	"github.com/pkg/errors"
)

type MachinesFunc func(monitor mntr.Monitor, desiredTree *tree.Tree, orbID string) (machineIDs []string, machines map[string]infra.Machine, err error)

func ListMachines(operarorLabels *labels.Operator) MachinesFunc {
	return func(monitor mntr.Monitor, desiredTree *tree.Tree, orbID string) (machineIDs []string, machines map[string]infra.Machine, err error) {
		defer func() {
			err = errors.Wrapf(err, "building %s failed", desiredTree.Common.Kind)
		}()

		desiredKind, err := ParseDesiredV0(desiredTree)
		if err != nil {
			return nil, nil, errors.Wrap(err, "parsing desired state failed")
		}
		desiredTree.Parsed = desiredKind

		machines = make(map[string]infra.Machine, 0)
		machineIDs = make([]string, 0)

		for clusterID, clusterTree := range desiredKind.Clusters {
			clusterCurrent := &tree.Tree{}
			_, _, _, _, _, err := clusters.GetQueryAndDestroyFuncs(
				monitor,
				operarorLabels,
				clusterID,
				clusterTree,
				true,
				false,
				false,
				clusterCurrent,
				nil,
				nil,
				nil,
				nil,
			)
			if err != nil {
				return nil, nil, err
			}
		}

		for provID, providerTree := range desiredKind.Providers {

			providerMachines, err := providers.ListMachines(
				monitor.WithFields(map[string]interface{}{"provider": provID}),
				providerTree,
				provID,
				orbID,
			)
			if err != nil {
				return nil, nil, err
			}

			for id, providerMachine := range providerMachines {
				machineID := provID + "." + id
				machineIDs = append(machineIDs, machineID)
				machines[machineID] = providerMachine
			}
		}

		return machineIDs, machines, nil
	}
}
