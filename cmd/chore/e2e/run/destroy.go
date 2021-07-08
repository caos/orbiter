package main

import (
	"context"
	"strings"
	"time"
)

var _ testFunc = destroy

func destroy(_ *testSpecs, settings programSettings, conditions *conditions) interactFunc {

	return func(ctx context.Context, _ uint8, newOrbctl newOrbctlCommandFunc) error {

		destroyCtx, destroyCtxCancel := context.WithTimeout(ctx, 5*time.Minute)
		defer destroyCtxCancel()

		cmd := newOrbctl(destroyCtx)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			panic(err)
		}

		var confirmed bool

		conditions.testCase = nil

		return runCommand(settings, orbctl.strPtr(), nil, func(line string) {
			if !confirmed && strings.HasPrefix(line, "Are you absolutely sure") {
				confirmed = true
				if _, err := stdin.Write([]byte("y\n")); err != nil {
					panic(err)
				}
			}
		}, cmd, "--gitops", "destroy")
	}
}
