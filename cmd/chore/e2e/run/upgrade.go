package main

import (
	"context"
	"fmt"
	"time"
)

func upgrade(k8sVersion string) testFunc {
	return func(_ *testSpecs, settings programSettings, conditions *conditions) interactFunc {

		conditions.kubernetes.Versions.Kubernetes = k8sVersion
		conditions.orbiter.watcher = watch(60*time.Minute, orbiter)
		conditions.testCase = nil

		return func(ctx context.Context, _ uint8, orbctl newOrbctlCommandFunc) error {

			return patch(ctx, settings, orbctl, fmt.Sprintf("clusters.%s.spec.versions.kubernetes", settings.orbID), k8sVersion)
		}
	}
}
