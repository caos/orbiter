package gce

import (
	"fmt"
	"sort"

	uuid "github.com/satori/go.uuid"
)

var _ ensureFWFunc = queryFirewall

func queryFirewall(cfg *svcConfig, firewalls []*firewall) ([]func() error, []func() error, error) {
	gceFirewalls, err := cfg.computeClient.Firewalls.
		List(cfg.projectID).
		Context(cfg.ctx).
		Filter(fmt.Sprintf(`network = "https://www.googleapis.com/compute/v1/%s"`, cfg.networkURL)).
		Fields("items(network,name,description,allowed,targetTags,sourceRanges)").
		Do()
	if err != nil {
		return nil, nil, err
	}

	var ensure []func() error
createLoop:
	for _, fw := range firewalls {
		for _, gceFW := range gceFirewalls.Items {
			if fw.gce.Description == gceFW.Description {
				if gceFW.Allowed[0].Ports[0] != fw.gce.Allowed[0].Ports[0] ||
					!stringsEqual(gceFW.TargetTags, fw.gce.TargetTags) ||
					!stringsEqual(gceFW.SourceRanges, fw.gce.SourceRanges) {
					ensure = append(ensure, operateFunc(
						fw.log("Patching firewall", true),
						computeOpCall(cfg.computeClient.Firewalls.Patch(cfg.projectID, gceFW.Name, fw.gce).
							Context(cfg.ctx).
							RequestId(uuid.NewV1().String()).
							Do),
						toErrFunc(fw.log("Firewall patched", false)),
					))
				}
				continue createLoop
			}
		}
		fw.gce.Name = newName()
		ensure = append(ensure, operateFunc(
			fw.log("Creating firewall", true),
			computeOpCall(cfg.computeClient.Firewalls.
				Insert(cfg.projectID, fw.gce).
				Context(cfg.ctx).
				RequestId(uuid.NewV1().String()).
				Do),
			toErrFunc(fw.log("Firewall created", false)),
		))
	}

	var remove []func() error
removeLoop:
	for _, gceTp := range gceFirewalls.Items {
		for _, fw := range firewalls {
			if gceTp.Description == fw.gce.Description {
				continue removeLoop
			}
		}
		remove = append(remove, removeResourceFunc(cfg.monitor, "firewall", gceTp.Name, cfg.computeClient.Firewalls.
			Delete(cfg.projectID, gceTp.Name).
			Context(cfg.ctx).
			RequestId(uuid.NewV1().String()).
			Do))
	}
	return ensure, remove, nil
}

func stringsEqual(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	sort.Strings(first)
	sort.Strings(second)
	for idx, f := range first {
		if second[idx] != f {
			return false
		}
	}
	return true
}
