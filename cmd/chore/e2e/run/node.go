package main

import (
	"context"
	"fmt"
)

func someMasterNodeContextAndID(ctx context.Context, settings programSettings, newOrbctl newOrbctlCommandFunc) (string, string, error) {

	var (
		context = fmt.Sprintf("%s.management", settings.orbID)
		id      string
	)
	return context, id, runCommand(settings, orbctl.strPtr(), nil, func(line string) {
		id = line
	}, newOrbctl(ctx), "--gitops", "nodes", "list", "--context", context, "--column", "id")
}
