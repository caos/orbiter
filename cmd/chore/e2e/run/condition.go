package main

import (
	"context"
	"fmt"
	"time"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/kubernetes"
)

type conditions struct {
	kubernetes *kubernetes.Spec
	testCase   *condition
	boom       *condition
	orbiter    *condition
}

func (c *conditions) desiredMasters() uint8 {
	return uint8(c.kubernetes.ControlPlane.Nodes)
}

func (c *conditions) desiredWorkers() uint8 {
	var workers uint8
	for _, pool := range c.kubernetes.Workers {
		workers += uint8(pool.Nodes)
	}
	return workers
}

func zeroConditions() *conditions { return &conditions{kubernetes: &kubernetes.Spec{}} }

type condition struct {
	checks  checksFunc
	watcher watcher
}

type checksFunc func(context.Context, newKubectlCommandFunc, currentOrbiter, common.NodeAgentsCurrentKind) error

type currentOrbiter struct {
	Clusters map[string]struct {
		Current kubernetes.CurrentCluster
	}
	Providers map[string]struct {
		Current struct {
			Ingresses struct {
				Httpsingress infra.Address
				Httpingress  infra.Address
				Kubeapi      infra.Address
			}
		}
	}
}

func (c *currentOrbiter) cluster(settings programSettings) (cc kubernetes.CurrentCluster, err error) {
	clusters, ok := c.Clusters[settings.orbID]
	if !ok {
		return cc, fmt.Errorf("cluster %s not found in current state", settings.orbID)
	}
	return clusters.Current, nil
}

type watcher struct {
	timeout              time.Duration
	selector             string
	logPrefix            operatorPrefix
	checkWhenLogContains string
}

type operatorPrefix string

func (o operatorPrefix) strPtr() *string {
	str := string(o)
	return &str
}

const (
	orbctl  operatorPrefix = "orbctl: "
	orbiter operatorPrefix = "ORBITER: "
	boom    operatorPrefix = "BOOM: "
)

func watch(timeout time.Duration, operator operatorPrefix) watcher {

	w := watcher{
		timeout:   timeout,
		logPrefix: operator,
	}

	switch operator {
	case orbiter:
		w.selector = "app.kubernetes.io/name=orbiter"
		w.checkWhenLogContains = "Desired state is ensured"
	case boom:
		w.selector = "app.kubernetes.io/name=boom"
		w.checkWhenLogContains = "Iteration done"
	case orbctl:
		panic("orbctl must be watched by reading its standard output")
	}

	return w
}
