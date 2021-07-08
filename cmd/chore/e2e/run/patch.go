package main

import (
	"context"
	"time"
)

func patch(ctx context.Context, settings programSettings, newOrbctl newOrbctlCommandFunc, path, value string) error {

	patchCtx, patchCancel := context.WithTimeout(ctx, 1*time.Minute)
	defer patchCancel()

	return runCommand(settings, orbctl.strPtr(), nil, nil, newOrbctl(patchCtx), "--gitops", "file", "patch", "orbiter.yml", path, "--value", value, "--exact")
}
