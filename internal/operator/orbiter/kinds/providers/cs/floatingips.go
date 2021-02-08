package cs

import (
	"net/http"
	"strconv"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"

	"github.com/cloudscale-ch/cloudscale-go-sdk"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers/dynamic"
)

func queryFloatingIPs(context *svcConfig, loadbalancing map[string][]*dynamic.VIP, writeTo *Current) ([]func() error, []func() error, map[string]bool, error) {

	haveUnassignedVIPs := make(map[string]bool)
	floatingIPs, err := context.client.FloatingIPs.List(context.ctx, func(r *http.Request) {
		params := r.URL.Query()
		params["orb"] = []string{context.orbID}
		params["provider"] = []string{context.providerID}
	})
	if err != nil {
		return nil, nil, nil, err
	}

	var ensure []func() error
createLoop:
	for hostPool, vips := range loadbalancing {
		for vipIdx := range vips {
			vip := vips[vipIdx]
			alreadyExists := false
			for transportIdx := range vip.Transport {
				transport := vip.Transport[transportIdx]
				initCurrent(writeTo, transport)

				for floatingIPIdx := range floatingIPs {
					floatingIP := floatingIPs[floatingIPIdx]
					if ensureCurrentIngress(floatingIP, hostPool, vipIdx, writeTo, transport) {
						alreadyExists = true
						if floatingIP.Server == nil || floatingIP.Server.UUID == "" {
							haveUnassignedVIPs[hostPool] = true
						}
					}
				}
			}
			if alreadyExists {
				continue createLoop
			}
			ensure = append(ensure, func(hostPool string, vipIdx int) func() error {
				return func() error {
					_, err := context.client.FloatingIPs.Create(context.ctx, &cloudscale.FloatingIPCreateRequest{
						RegionalResourceRequest: cloudscale.RegionalResourceRequest{},
						TaggedResourceRequest: cloudscale.TaggedResourceRequest{Tags: map[string]string{
							"orb":      context.orbID,
							"provider": context.providerID,
							"pool":     hostPool,
							"idx":      strconv.Itoa(vipIdx),
						}},
						IPVersion:      4,
						Server:         "",
						Type:           "regional",
						PrefixLength:   0,
						ReversePointer: "",
					})
					if err != nil {
						return err
					}
					return nil
				}
			}(hostPool, vipIdx))
		}
	}

	var remove []func() error

removeLoop:
	for floatingIPIdx := range floatingIPs {
		floatingIP := floatingIPs[floatingIPIdx]
		matches := false
		for hostPool, vips := range loadbalancing {
			for vipIdx := range vips {
				vip := vips[vipIdx]
				for transpIdx := range vip.Transport {
					transport := vip.Transport[transpIdx]
					initCurrent(writeTo, transport)
					matches = ensureCurrentIngress(floatingIP, hostPool, vipIdx, writeTo, transport) || matches
				}
			}
		}
		if matches {
			continue removeLoop
		}
		remove = append(remove, func(ip string) func() error {
			return func() error {
				return context.client.FloatingIPs.Delete(context.ctx, ip)
			}
		}(floatingIP.IP()))
	}
	return ensure, remove, haveUnassignedVIPs, nil
}

func initCurrent(writeTo *Current, transport *dynamic.Transport) {
	if writeTo.Current.Ingresses == nil {
		writeTo.Current.Ingresses = make(map[string]*infra.Address)
	}
	if writeTo.Current.Ingresses[transport.Name] == nil {
		writeTo.Current.Ingresses[transport.Name] = &infra.Address{}
	}
}

func ensureCurrentIngress(floatingIP cloudscale.FloatingIP, hostPool string, vipIdx int, writeTo *Current, transport *dynamic.Transport) bool {
	matches := false
	if floatingIP.Tags["pool"] == hostPool && floatingIP.Tags["idx"] == strconv.Itoa(vipIdx) {
		matches = true
		writeTo.Current.Ingresses[transport.Name].Location = floatingIP.IP()
		writeTo.Current.Ingresses[transport.Name].FrontendPort = uint16(transport.FrontendPort)
		writeTo.Current.Ingresses[transport.Name].BackendPort = uint16(transport.BackendPort)
	}
	return matches
}
