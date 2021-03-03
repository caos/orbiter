package cloudflare

import (
	"github.com/cloudflare/cloudflare-go"
	"time"
)

type LoadBalancer struct {
	ID                        string                     `json:"id,omitempty"`
	CreatedOn                 *time.Time                 `json:"created_on,omitempty"`
	ModifiedOn                *time.Time                 `json:"modified_on,omitempty"`
	Description               string                     `json:"description"`
	Name                      string                     `json:"name"`
	TTL                       int                        `json:"ttl,omitempty"`
	FallbackPool              string                     `json:"fallback_pool"`
	DefaultPools              []string                   `json:"default_pools"`
	RegionPools               map[string][]string        `json:"region_pools"`
	PopPools                  map[string][]string        `json:"pop_pools"`
	Proxied                   bool                       `json:"proxied"`
	Enabled                   *bool                      `json:"enabled,omitempty"`
	Persistence               string                     `json:"session_affinity,omitempty"`
	PersistenceTTL            int                        `json:"session_affinity_ttl,omitempty"`
	SessionAffinityAttributes *SessionAffinityAttributes `json:"session_affinity_attributes,omitempty"`

	// SteeringPolicy controls pool selection logic.
	// "off" select pools in DefaultPools order
	// "geo" select pools based on RegionPools/PopPools
	// "dynamic_latency" select pools based on RTT (requires health checks)
	// "random" selects pools in a random order
	// "" maps to "geo" if RegionPools or PopPools have entries otherwise "off"
	SteeringPolicy string `json:"steering_policy,omitempty"`
}

// SessionAffinityAttributes represents the fields used to set attributes in a load balancer session affinity cookie.
type SessionAffinityAttributes struct {
	SameSite string `json:"samesite,omitempty"`
	Secure   string `json:"secure,omitempty"`
}

func (c *Cloudflare) CreateLoadBalancer(domain string, lb *LoadBalancer) (*LoadBalancer, error) {
	id, err := c.api.ZoneIDByName(domain)
	if err != nil {
		return nil, err
	}

	createdLb, err := c.api.CreateLoadBalancer(id, internalLbToLb(lb))

	if err != nil {
		return nil, err
	}

	return lbToInternalLb(createdLb), err
}

func (c *Cloudflare) UpdateLoadBalancer(domain string, lb *LoadBalancer) (*LoadBalancer, error) {
	id, err := c.api.ZoneIDByName(domain)
	if err != nil {
		return nil, err
	}

	updated, err := c.api.ModifyLoadBalancer(id, internalLbToLb(lb))
	if err != nil {
		return nil, err
	}

	return lbToInternalLb(updated), err
}

func (c *Cloudflare) DeleteLoadBalancer(domain string, lbID string) error {
	id, err := c.api.ZoneIDByName(domain)
	if err != nil {
		return err
	}

	return c.api.DeleteLoadBalancer(id, lbID)
}

func (c *Cloudflare) ListLoadBalancers(domain string) ([]*LoadBalancer, error) {
	id, err := c.api.ZoneIDByName(domain)
	if err != nil {
		return nil, err
	}

	lbs, err := c.api.ListLoadBalancers(id)
	if err != nil {
		return nil, err
	}
	return lbsToInternalLbs(lbs), nil
}

func lbsToInternalLbs(lbs []cloudflare.LoadBalancer) []*LoadBalancer {
	ret := make([]*LoadBalancer, 0)
	for _, lb := range lbs {
		ret = append(ret, lbToInternalLb(lb))
	}
	return ret
}

func lbToInternalLb(lb cloudflare.LoadBalancer) *LoadBalancer {
	return &LoadBalancer{
		ID:                        lb.ID,
		CreatedOn:                 lb.CreatedOn,
		ModifiedOn:                lb.ModifiedOn,
		Description:               lb.Description,
		Name:                      lb.Name,
		TTL:                       lb.TTL,
		FallbackPool:              lb.FallbackPool,
		DefaultPools:              lb.DefaultPools,
		RegionPools:               lb.RegionPools,
		PopPools:                  lb.PopPools,
		Proxied:                   lb.Proxied,
		Enabled:                   lb.Enabled,
		Persistence:               lb.Persistence,
		PersistenceTTL:            lb.PersistenceTTL,
		SessionAffinityAttributes: saaToInternalSaa(lb.SessionAffinityAttributes),
		SteeringPolicy:            lb.SteeringPolicy,
	}
}

func saaToInternalSaa(saa *cloudflare.SessionAffinityAttributes) *SessionAffinityAttributes {
	return &SessionAffinityAttributes{
		SameSite: saa.SameSite,
		Secure:   saa.Secure,
	}
}

func internalLbToLb(lb *LoadBalancer) cloudflare.LoadBalancer {
	return cloudflare.LoadBalancer{
		ID:                        lb.ID,
		CreatedOn:                 lb.CreatedOn,
		ModifiedOn:                lb.ModifiedOn,
		Description:               lb.Description,
		Name:                      lb.Name,
		TTL:                       lb.TTL,
		FallbackPool:              lb.FallbackPool,
		DefaultPools:              lb.DefaultPools,
		RegionPools:               lb.RegionPools,
		PopPools:                  lb.PopPools,
		Proxied:                   lb.Proxied,
		Enabled:                   lb.Enabled,
		Persistence:               lb.Persistence,
		PersistenceTTL:            lb.PersistenceTTL,
		SessionAffinityAttributes: internalSaaToSaa(lb.SessionAffinityAttributes),
		SteeringPolicy:            lb.SteeringPolicy,
	}
}

func internalSaaToSaa(saa *SessionAffinityAttributes) *cloudflare.SessionAffinityAttributes {
	if saa == nil {
		return nil
	}
	return &cloudflare.SessionAffinityAttributes{
		SameSite: saa.SameSite,
		Secure:   saa.Secure,
	}
}
