package app

import (
	"github.com/caos/orbos/internal/operator/networking/kinds/networking/legacycf/cloudflare"
	"reflect"
	"strings"
)

func getPoolName(domain, region, clusterID string) string {
	return strings.Join([]string{clusterID, region, strings.ReplaceAll(domain, ".", "-")}, "-")
}

func (a *App) EnsureLoadBalancerPools(id string, pools []*cloudflare.LoadBalancerPool) (func() error, error) {
	destroy := func() error {
		return nil
	}
	currentPools, err := a.cloudflare.ListLoadBalancerPools()
	if err != nil {
		return nil, err
	}

	filteredCurrentPools := make([]*cloudflare.LoadBalancerPool, 0)
	for _, currentPool := range currentPools {
		if currentPool.Description == id {
			filteredCurrentPools = append(filteredCurrentPools, currentPool)
		}
	}

	deletePools := getLoadBalancerPoolsToDelete(filteredCurrentPools, pools)
	if deletePools != nil && len(deletePools) > 0 {
		destroy = func() error {
			for _, pool := range deletePools {
				if err := a.cloudflare.DeleteLoadBalancerPools(pool); err != nil {
					return err
				}
			}
			return nil
		}
	}

	createPools := getLoadBalancerPoolsToCreate(currentPools, pools)
	if createPools != nil && len(createPools) > 0 {
		for _, pool := range createPools {
			created, err := a.cloudflare.CreateLoadBalancerPools(pool)
			if err != nil {
				return nil, err
			}
			pool.ID = created.ID
		}
	}

	updatePools := getLoadBalancerPoolsToUpdate(currentPools, pools)
	if updatePools != nil && len(updatePools) > 0 {
		for _, pool := range updatePools {
			updated, err := a.cloudflare.UpdateLoadBalancerPools(pool)
			if err != nil {
				return nil, err
			}
			pool.ID = updated.ID
		}
	}

	return destroy, nil
}

func getLoadBalancerPoolsToDelete(currentPools []*cloudflare.LoadBalancerPool, pools []*cloudflare.LoadBalancerPool) []string {
	deletePools := make([]string, 0)
	for _, currentPool := range currentPools {
		found := false
		if pools != nil {
			for _, pool := range pools {
				if currentPool.Name == pool.Name {
					found = true
				}
			}
		}

		if !found {
			deletePools = append(deletePools, currentPool.ID)
		}
	}

	return deletePools
}

func getLoadBalancerPoolsToCreate(currentPools []*cloudflare.LoadBalancerPool, pools []*cloudflare.LoadBalancerPool) []*cloudflare.LoadBalancerPool {
	createPools := make([]*cloudflare.LoadBalancerPool, 0)

	if pools != nil {
		for _, pool := range pools {
			found := false
			for _, currentPool := range currentPools {
				if currentPool.Name == pool.Name {
					found = true
					break
				}
			}
			if found == false {
				createPools = append(createPools, pool)
			}
		}
	}

	return createPools
}

func getLoadBalancerPoolsToUpdate(currentPools []*cloudflare.LoadBalancerPool, pools []*cloudflare.LoadBalancerPool) []*cloudflare.LoadBalancerPool {
	updatePools := make([]*cloudflare.LoadBalancerPool, 0)

	if pools != nil {
		for _, pool := range pools {
			for _, currentPool := range currentPools {
				if currentPool.Name == pool.Name &&
					!reflect.DeepEqual(currentPool, pool) {
					pool.ID = currentPool.ID
					updatePools = append(updatePools, pool)
				}
			}
		}
	}

	return updatePools
}
