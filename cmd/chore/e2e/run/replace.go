package main

import (
	"context"
	"fmt"
	"time"

	"github.com/caos/orbos/internal/operator/common"
)

var _ testFunc = replace

func replace(_ *testSpecs, settings programSettings, conditions *conditions) interactFunc {
	return func(ctx context.Context, _ uint8, newOrbctl newOrbctlCommandFunc) error {

		replaceCtx, replaceCancel := context.WithTimeout(ctx, 1*time.Minute)
		defer replaceCancel()

		nodeContext, nodeID, err := someMasterNodeContextAndID(replaceCtx, settings, newOrbctl)
		if err != nil {
			return err
		}

		// as we don't know the machines name that a skipped test replaced originally (variable "nodeID"), we can't check this anymore in downstream tests.
		conditions.testCase = &condition{
			watcher: watch(20*time.Minute, orbiter),
			checks: func(_ context.Context, _ newKubectlCommandFunc, _ currentOrbiter, current common.NodeAgentsCurrentKind) error {
				_, ok := current.Current.Get(nodeID)
				if ok {
					return fmt.Errorf("nodeagent %s still has a current state", nodeID)
				}
				return nil
			},
		}

		return runCommand(settings, orbctl.strPtr(), nil, nil, newOrbctl(replaceCtx), "--gitops", "node", "replace", fmt.Sprintf("%s.%s", nodeContext, nodeID))
	}
}
