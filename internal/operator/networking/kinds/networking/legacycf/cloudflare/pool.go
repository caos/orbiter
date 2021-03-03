package cloudflare

import (
	"github.com/cloudflare/cloudflare-go"
	"time"
)

type LoadBalancerPool struct {
	ID                string                `json:"id,omitempty"`
	CreatedOn         *time.Time            `json:"created_on,omitempty"`
	ModifiedOn        *time.Time            `json:"modified_on,omitempty"`
	Description       string                `json:"description"`
	Name              string                `json:"name"`
	Enabled           bool                  `json:"enabled"`
	MinimumOrigins    int                   `json:"minimum_origins,omitempty"`
	Monitor           string                `json:"monitor,omitempty"`
	Origins           []*LoadBalancerOrigin `json:"origins"`
	NotificationEmail string                `json:"notification_email,omitempty"`

	// CheckRegions defines the geographic region(s) from where to run health-checks from - e.g. "WNAM", "WEU", "SAF", "SAM".
	// Providing a null/empty value means "all regions", which may not be available to all plan types.
	CheckRegions []string `json:"check_regions"`
}

type LoadBalancerOrigin struct {
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Enabled bool    `json:"enabled"`
	Weight  float64 `json:"weight"`
}

func (c *Cloudflare) GetLoadBalancerPoolDetails(ID string) (*LoadBalancerPool, error) {
	pool, err := c.api.LoadBalancerPoolDetails(ID)

	if err != nil {
		return nil, err
	}

	return poolToInternalPool(pool), err
}

func (c *Cloudflare) CreateLoadBalancerPools(pool *LoadBalancerPool) (*LoadBalancerPool, error) {
	createdPool, err := c.api.CreateLoadBalancerPool(internalPoolToPool(pool))

	if err != nil {
		return nil, err
	}

	return poolToInternalPool(createdPool), err
}

func (c *Cloudflare) UpdateLoadBalancerPools(pool *LoadBalancerPool) (*LoadBalancerPool, error) {
	updatedPool, err := c.api.ModifyLoadBalancerPool(internalPoolToPool(pool))
	if err != nil {
		return nil, err
	}

	return poolToInternalPool(updatedPool), err
}

func (c *Cloudflare) DeleteLoadBalancerPools(poolID string) error {
	return c.api.DeleteLoadBalancerPool(poolID)
}

func (c *Cloudflare) ListLoadBalancerPools() ([]*LoadBalancerPool, error) {
	pools, err := c.api.ListLoadBalancerPools()
	if err != nil {
		return nil, err
	}
	return poolsToInternalPools(pools), nil
}

func poolsToInternalPools(pools []cloudflare.LoadBalancerPool) []*LoadBalancerPool {
	retPools := make([]*LoadBalancerPool, 0)
	for _, pool := range pools {
		retPools = append(retPools, poolToInternalPool(pool))
	}
	return retPools
}

func poolToInternalPool(pool cloudflare.LoadBalancerPool) *LoadBalancerPool {
	return &LoadBalancerPool{
		ID:                pool.ID,
		CreatedOn:         pool.CreatedOn,
		ModifiedOn:        pool.ModifiedOn,
		Description:       pool.Description,
		Name:              pool.Name,
		Enabled:           pool.Enabled,
		MinimumOrigins:    pool.MinimumOrigins,
		Monitor:           pool.Monitor,
		Origins:           originsToInternalOrigins(pool.Origins),
		NotificationEmail: pool.NotificationEmail,
		CheckRegions:      pool.CheckRegions,
	}
}

func originsToInternalOrigins(origins []cloudflare.LoadBalancerOrigin) []*LoadBalancerOrigin {
	internalOrigins := make([]*LoadBalancerOrigin, 0)
	for _, origin := range origins {
		internalOrigins = append(internalOrigins, &LoadBalancerOrigin{
			Name:    origin.Name,
			Address: origin.Address,
			Enabled: origin.Enabled,
			Weight:  origin.Weight,
		})
	}
	return internalOrigins
}

func internalPoolToPool(pool *LoadBalancerPool) cloudflare.LoadBalancerPool {
	return cloudflare.LoadBalancerPool{
		ID:                pool.ID,
		CreatedOn:         pool.CreatedOn,
		ModifiedOn:        pool.ModifiedOn,
		Description:       pool.Description,
		Name:              pool.Name,
		Enabled:           pool.Enabled,
		MinimumOrigins:    pool.MinimumOrigins,
		Monitor:           pool.Monitor,
		Origins:           internalOriginsToOrigins(pool.Origins),
		NotificationEmail: pool.NotificationEmail,
		CheckRegions:      pool.CheckRegions,
	}
}

func internalOriginsToOrigins(internalOrigins []*LoadBalancerOrigin) []cloudflare.LoadBalancerOrigin {
	origins := make([]cloudflare.LoadBalancerOrigin, 0)
	for _, origin := range internalOrigins {
		origins = append(origins, cloudflare.LoadBalancerOrigin{
			Name:    origin.Name,
			Address: origin.Address,
			Enabled: origin.Enabled,
			Weight:  origin.Weight,
		})
	}
	return origins
}
