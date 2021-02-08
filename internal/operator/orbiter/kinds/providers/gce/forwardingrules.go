package gce

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
)

var _ ensureLBFunc = queryForwardingRules

func queryForwardingRules(cfg *svcConfig, loadbalancing []*normalizedLoadbalancer) ([]func() error, []func() error, error) {
	gceRules, err := cfg.computeClient.ForwardingRules.
		List(cfg.projectID, cfg.desired.Region).
		Context(cfg.ctx).
		Filter(fmt.Sprintf(`description : "orb=%s;provider=%s*"`, cfg.orbID, cfg.providerID)).
		Fields("items(description,name,target,portRange,IPAddress)").
		Do()
	if err != nil {
		return nil, nil, err
	}

	var ensure []func() error
	assignRefs := func(lb *normalizedLoadbalancer) {
		lb.forwardingRule.gce.IPAddress = lb.address.gce.Address
		lb.forwardingRule.gce.Target = lb.targetPool.gce.SelfLink
	}

createLoop:
	for _, lb := range loadbalancing {
		for _, gceRule := range gceRules.Items {
			if gceRule.Description == lb.forwardingRule.gce.Description && gceRule.PortRange == lb.forwardingRule.gce.PortRange {
				assignRefs(lb)
				lb.forwardingRule.gce.Name = gceRule.Name
				continue createLoop
			}
		}

		lb.forwardingRule.gce.Name = newName()
		ensure = append(ensure, operateFunc(
			func(l *normalizedLoadbalancer) func() {
				return func() {
					assignRefs(l)
					l.forwardingRule.log("Creating forwarding rule", true)()
				}
			}(lb),
			computeOpCall(cfg.computeClient.ForwardingRules.
				Insert(cfg.projectID, cfg.desired.Region, lb.forwardingRule.gce).
				Context(cfg.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			toErrFunc(lb.forwardingRule.log("Forwarding rule created", false)),
		))
	}

	var remove []func() error

removeLoop:
	for _, rule := range gceRules.Items {
		for _, lb := range loadbalancing {
			if rule.Description == lb.forwardingRule.gce.Description {
				continue removeLoop
			}
		}
		remove = append(remove, removeResourceFunc(
			cfg.monitor, "forwarding rule", rule.Name, cfg.computeClient.ForwardingRules.
				Delete(cfg.projectID, cfg.desired.Region, rule.Name).
				Context(cfg.ctx).
				RequestId(uuid.NewV1().String()).
				Do,
		))
	}
	return ensure, remove, nil
}
