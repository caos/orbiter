package gce

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
)

var _ ensureLBFunc = queryHealthchecks

func queryHealthchecks(cfg *svcConfig, loadbalancing []*normalizedLoadbalancer) ([]func() error, []func() error, error) {
	gceHealthchecks, err := cfg.computeClient.HttpHealthChecks.
		List(cfg.projectID).
		Context(cfg.ctx).
		Filter(fmt.Sprintf(`description : "orb=%s;provider=%s*"`, cfg.orbID, cfg.providerID)).
		Fields("items(description,name,port,requestPath,selfLink)").
		Do()
	if err != nil {
		return nil, nil, err
	}

	var ensure []func() error

createLoop:
	for _, lb := range loadbalancing {

		for _, gceHC := range gceHealthchecks.Items {
			if gceHC.Description == lb.healthcheck.gce.Description {
				lb.healthcheck.gce.Name = gceHC.Name
				lb.healthcheck.gce.SelfLink = gceHC.SelfLink
				if gceHC.Port != lb.healthcheck.gce.Port || gceHC.RequestPath != lb.healthcheck.gce.RequestPath {
					ensure = append(ensure, operateFunc(
						lb.healthcheck.log("Patching healthcheck", true),
						computeOpCall(cfg.computeClient.HttpHealthChecks.Patch(cfg.projectID, gceHC.Name, lb.healthcheck.gce).
							Context(cfg.ctx).
							RequestId(uuid.NewV1().String()).
							Do),
						toErrFunc(lb.healthcheck.log("Healthcheck patched", false)),
					))
				}

				continue createLoop
			}
		}
		lb.healthcheck.gce.Name = newName()
		ensure = append(ensure, operateFunc(
			lb.healthcheck.log("Creating healthcheck", true),
			computeOpCall(cfg.computeClient.HttpHealthChecks.
				Insert(cfg.projectID, lb.healthcheck.gce).
				Context(cfg.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			func(hc *healthcheck) func() error {
				return func() error {
					newHC, newHCErr := cfg.computeClient.HttpHealthChecks.Get(cfg.projectID, hc.gce.Name).
						Context(cfg.ctx).
						Fields("selfLink").
						Do()
					if newHCErr != nil {
						return newHCErr
					}

					hc.gce.SelfLink = newHC.SelfLink
					hc.log("Healthcheck created", false)()
					return nil
				}
			}(lb.healthcheck),
		))
	}

	var remove []func() error
removeLoop:
	for _, gceHC := range gceHealthchecks.Items {
		for _, lb := range loadbalancing {
			if gceHC.Description == lb.healthcheck.gce.Description {
				continue removeLoop
			}
		}
		remove = append(remove, removeResourceFunc(cfg.monitor, "healthcheck", gceHC.Name, cfg.computeClient.HttpHealthChecks.
			Delete(cfg.projectID, gceHC.Name).
			Context(cfg.ctx).
			RequestId(uuid.NewV1().String()).
			Do))
	}
	return ensure, remove, nil
}
