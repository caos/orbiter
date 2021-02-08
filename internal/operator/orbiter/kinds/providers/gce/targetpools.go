package gce

import (
	"fmt"

	uuid "github.com/satori/go.uuid"

	"google.golang.org/api/compute/v1"
)

var _ ensureLBFunc = queryTargetPools

func queryTargetPools(cfg *svcConfig, loadbalancing []*normalizedLoadbalancer) ([]func() error, []func() error, error) {
	gcePools, err := cfg.computeClient.TargetPools.
		List(cfg.projectID, cfg.desired.Region).
		Context(cfg.ctx).
		Filter(fmt.Sprintf(`description : "orb=%s;provider=%s*"`, cfg.orbID, cfg.providerID)).
		Fields("items(description,name,instances,selfLink,name)").
		Do()
	if err != nil {
		return nil, nil, err
	}

	assignRefs := func(lb *normalizedLoadbalancer) {
		lb.targetPool.gce.HealthChecks = []string{lb.healthcheck.gce.SelfLink}
	}

	var ensure []func() error

createLoop:
	for _, lb := range loadbalancing {
		for _, gceTp := range gcePools.Items {
			if gceTp.Description == lb.targetPool.gce.Description {
				lb.targetPool.gce.SelfLink = gceTp.SelfLink
				lb.targetPool.gce.Name = gceTp.Name
				assignRefs(lb)
				if len(gceTp.HealthChecks) > 0 && gceTp.HealthChecks[0] != lb.targetPool.gce.HealthChecks[0] {
					ensure = append(ensure, operateFunc(
						lb.targetPool.log("Removing healthcheck", true, nil),
						computeOpCall(cfg.computeClient.TargetPools.RemoveHealthCheck(
							cfg.projectID,
							cfg.desired.Region,
							gceTp.Name,
							&compute.TargetPoolsRemoveHealthCheckRequest{HealthChecks: []*compute.HealthCheckReference{{HealthCheck: gceTp.HealthChecks[0]}}},
						).
							Context(cfg.ctx).
							RequestId(uuid.NewV1().String()).
							Do),
						toErrFunc(lb.targetPool.log("Healthcheck removed", false, nil)),
					))

					ensure = append(ensure, operateFunc(
						lb.targetPool.log("Adding healthcheck", true, nil),
						computeOpCall(cfg.computeClient.TargetPools.AddHealthCheck(
							cfg.projectID,
							cfg.desired.Region,
							gceTp.Name,
							&compute.TargetPoolsAddHealthCheckRequest{HealthChecks: []*compute.HealthCheckReference{{HealthCheck: lb.healthcheck.gce.SelfLink}}},
						).
							Context(cfg.ctx).
							RequestId(uuid.NewV1().String()).
							Do),
						toErrFunc(lb.targetPool.log("Healthcheck added", false, nil)),
					))
				}
				continue createLoop
			}
		}

		lb.targetPool.gce.Name = newName()

		ensure = append(ensure, operateFunc(
			func(l *normalizedLoadbalancer) func() {
				return func() {
					assignRefs(l)
					lb.targetPool.log("Creating target pool", true, nil)()
				}
			}(lb),
			computeOpCall(cfg.computeClient.TargetPools.
				Insert(cfg.projectID, cfg.desired.Region, lb.targetPool.gce).
				Context(cfg.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			func(pool *targetPool) func() error {
				return func() error {
					newTP, err := cfg.computeClient.TargetPools.Get(cfg.projectID, cfg.desired.Region, pool.gce.Name).
						Context(cfg.ctx).
						Fields("selfLink").
						Do()
					if err != nil {
						return err
					}

					pool.gce.SelfLink = newTP.SelfLink
					pool.log("Target pool created", false, nil)()
					return nil
				}
			}(lb.targetPool),
		))
	}

	var remove []func() error
removeLoop:
	for _, gceTp := range gcePools.Items {
		for _, lb := range loadbalancing {
			if gceTp.Description == lb.targetPool.gce.Description {
				continue removeLoop
			}
		}
		remove = append(remove, removeResourceFunc(cfg.monitor, "target pool", gceTp.Name, cfg.computeClient.TargetPools.
			Delete(cfg.projectID, cfg.desired.Region, gceTp.Name).
			Context(cfg.ctx).
			RequestId(uuid.NewV1().String()).
			Do))
	}
	return ensure, remove, nil
}
