package app

import (
	"github.com/caos/orbos/internal/operator/networking/kinds/networking/legacycf/cloudflare"
	"github.com/caos/orbos/internal/operator/networking/kinds/networking/legacycf/config"
)

func (a *App) EnsureLoadBalancers(id string, clusterID, region, domain string, lbs []*cloudflare.LoadBalancer) error {
	currentLbs, err := a.cloudflare.ListLoadBalancers(domain)
	if err != nil {
		return err
	}

	//only try to manage lbs which have pools in them managed by this operator
	filteredCurrentLbs := make([]*cloudflare.LoadBalancer, 0)
	for _, currentLb := range currentLbs {
		filteredPool := filterSameID(a.cloudflare, currentLb.DefaultPools, id)
		if filteredPool != nil && len(filteredPool) > 0 {
			filteredCurrentLbs = append(filteredCurrentLbs, currentLb)
		}
	}

	deleteLbs, updateLbs := getLoadBalancerToDelete(a.cloudflare, id, filteredCurrentLbs, lbs)
	if deleteLbs != nil && len(deleteLbs) > 0 {
		for _, lb := range deleteLbs {
			if err := a.cloudflare.DeleteLoadBalancer(domain, lb); err != nil {
				return err
			}
		}
	}

	createLbs := getLoadBalancerToCreate(currentLbs, lbs)
	if createLbs != nil && len(createLbs) > 0 {
		for _, lb := range createLbs {
			_, err := a.cloudflare.CreateLoadBalancer(domain, lb)
			if err != nil {
				return err
			}
		}
	}

	updateLbs = append(updateLbs, getLoadBalancerToUpdate(a.cloudflare, id, clusterID, region, domain, currentLbs, lbs)...)
	if updateLbs != nil && len(updateLbs) > 0 {
		for _, lb := range updateLbs {
			_, err := a.cloudflare.UpdateLoadBalancer(domain, lb)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func getLoadBalancerToDelete(
	cf *cloudflare.Cloudflare,
	id string,
	currentLbs []*cloudflare.LoadBalancer,
	lbs []*cloudflare.LoadBalancer,
) (
	[]string,
	[]*cloudflare.LoadBalancer,
) {
	deleteLbs := make([]string, 0)
	updateLbs := make([]*cloudflare.LoadBalancer, 0)

	for _, currentLb := range currentLbs {
		found := false
		if lbs != nil {
			for _, lb := range lbs {
				if currentLb.Name == lb.Name {
					found = true
					break
				}
			}
		}

		hasPoolWithOtherID := hasPoolsWithOtherID(cf, id, currentLb)
		//delete lbs with no pools managed by another operator
		if !found && !hasPoolWithOtherID {
			deleteLbs = append(deleteLbs, currentLb.ID)
			//otherwise just delete the pool and let the lb stand
		} else if !found && hasPoolWithOtherID {
			updateLb := currentLb
			updateLb.DefaultPools = filterNotSameID(cf, currentLb.DefaultPools, id)
			updateLb.FallbackPool = updateLb.DefaultPools[0]

			updateLbs = append(updateLbs, updateLb)
		}
	}

	return deleteLbs, updateLbs
}

func getLoadBalancerToCreate(currentLbs []*cloudflare.LoadBalancer, lbs []*cloudflare.LoadBalancer) []*cloudflare.LoadBalancer {
	createLbs := make([]*cloudflare.LoadBalancer, 0)

	if lbs == nil {
		return createLbs
	}

	for _, lb := range lbs {
		found := false
		for _, currentLb := range currentLbs {
			if currentLb.Name == lb.Name {
				found = true
				break
			}
		}
		if !found {
			createLbs = append(createLbs, lb)
		}
	}

	return createLbs
}

func getLoadBalancerToUpdate(
	cf *cloudflare.Cloudflare,
	id,
	clusterID,
	region,
	domain string,
	currentLbs []*cloudflare.LoadBalancer,
	lbs []*cloudflare.LoadBalancer,
) []*cloudflare.LoadBalancer {
	updateLbs := make([]*cloudflare.LoadBalancer, 0)

	if lbs == nil {
		return updateLbs
	}

	poolName := getPoolName(domain, region, clusterID)

	for _, lb := range lbs {
		for _, currentLb := range currentLbs {
			if currentLb.Name == config.GetLBName(domain) {
				containedRegion := false
				containedDefault := false
				for _, currentPool := range currentLb.DefaultPools {
					if currentPool == poolName {
						containedDefault = true
						break
					}
				}

			regionPoolsLoop:
				for currentRegion, currentPools := range currentLb.RegionPools {
					if currentRegion == region {
						for _, currentPool := range currentPools {
							if currentPool == poolName {
								containedRegion = true
								break regionPoolsLoop
							}
						}
					}
				}

				//only update the lb if a pool has to be added
				if containedDefault && containedRegion {
					continue
				}

				lb.ID = currentLb.ID
				// add already entered pools for update
				//all pools which are maintained by operators with other ids
				lb.DefaultPools = append(lb.DefaultPools, filterNotSameID(cf, currentLb.DefaultPools, id)...)

				// combine the defined region pools for update
				for currentRegion, currentPools := range currentLb.RegionPools {
					regionSlice, found := lb.RegionPools[currentRegion]
					if found {
						lb.DefaultPools = append(lb.DefaultPools, filterNotSameID(cf, currentLb.DefaultPools, id)...)

						regionSlice = append(regionSlice, currentPools...)
					} else {
						defPool := []string{}
						defPool = append(defPool, filterNotSameID(cf, currentPools, id)...)
						lb.RegionPools[currentRegion] = defPool
					}
				}
				updateLbs = append(updateLbs, lb)
			}
		}
	}

	return updateLbs
}

func hasPoolsWithOtherID(cf *cloudflare.Cloudflare, id string, lb *cloudflare.LoadBalancer) bool {
	filteredPools := filterNotSameID(cf, lb.DefaultPools, id)
	if filteredPools != nil && len(filteredPools) > 0 {
		return true
	}
	return false
}

func filterSameID(cf *cloudflare.Cloudflare, pools []string, id string) []string {
	ret := make([]string, 0)
	for _, poolID := range pools {
		poolDet, err := cf.GetLoadBalancerPoolDetails(poolID)
		if err != nil {
			return nil
		}
		if poolDet.Description == id {
			ret = append(ret, poolID)
		}
	}
	return ret
}

func filterNotSameID(cf *cloudflare.Cloudflare, pools []string, id string) []string {
	ret := make([]string, 0)
	for _, poolID := range pools {
		poolDet, err := cf.GetLoadBalancerPoolDetails(poolID)
		if err != nil {
			return nil
		}
		if poolDet.Description != id {
			ret = append(ret, poolID)
		}
	}
	return ret
}
