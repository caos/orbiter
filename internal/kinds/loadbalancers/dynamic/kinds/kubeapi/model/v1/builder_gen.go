// Code generated by "gen-kindstubs -parentpath=github.com/caos/orbiter/internal/kinds/loadbalancers/dynamic/kinds -versions=v1 -kind=orbiter.caos.ch/DynamicKubeAPILoadBalancer from file gen.go"; DO NOT EDIT.

package v1

import (
	"errors"

	"github.com/caos/orbiter/internal/core/operator/orbiter"
	"github.com/caos/orbiter/internal/kinds/loadbalancers/dynamic/kinds/kubeapi/model"
)

var build func(map[string]interface{}, *orbiter.Secrets, interface{}) (model.UserSpec, func(model.Config) ([]orbiter.Assembler, error))

func Build(spec map[string]interface{}, secrets *orbiter.Secrets, dependant interface{}) (model.UserSpec, func(cfg model.Config) ([]orbiter.Assembler, error)) {
	if build != nil {
		return build(spec, secrets, dependant)
	}
	return model.UserSpec{}, func(_ model.Config) ([]orbiter.Assembler, error) {
		return nil, errors.New("Version v1 for kind orbiter.caos.ch/DynamicKubeAPILoadBalancer is not yet supported")
	}
}
