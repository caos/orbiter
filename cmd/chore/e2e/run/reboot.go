package main

import (
	"context"
	"fmt"
	"time"

	"github.com/caos/orbos/internal/operator/common"
)

var _ testFunc = reboot

func reboot(_ *testSpecs, settings programSettings, conditions *conditions) interactFunc {

	return func(ctx context.Context, _ uint8, orbctl newOrbctlCommandFunc) error {

		rebootCtx, rebootCancel := context.WithTimeout(ctx, 1*time.Minute)
		defer rebootCancel()

		since := time.Now()

		nodeContext, nodeID, err := someMasterNodeContextAndID(rebootCtx, settings, orbctl)
		if err != nil {
			return err
		}

		// as we don't know the time when a skipped test was run originally (variable "since"), we can't check this anymore in downstream tests.
		conditions.testCase = &condition{
			watcher: watch(10*time.Minute, orbiter),
			checks: func(_ context.Context, _ newKubectlCommandFunc, _ currentOrbiter, current common.NodeAgentsCurrentKind) error {
				nodeagent, ok := current.Current.Get(nodeID)
				if !ok {
					return fmt.Errorf("nodeagent %s not found", nodeID)
				}
				if nodeagent.Booted.Before(since) {
					return fmt.Errorf("nodeagent %s has latest boot at %s which is before %s", nodeID, nodeagent.Booted.Format(time.RFC822), since.Format(time.RFC822))
				}
				return nil
			},
		}

		return runCommand(settings, orbiter.strPtr(), nil, nil, orbctl(rebootCtx), "--gitops", "node", "reboot", fmt.Sprintf("%s.%s", nodeContext, nodeID))
	}
}
