// Code generated by "gen-kindstubs -parentpath=github.com/caos/orbiter/internal/kinds/loadbalancers -versions=v0 -kind=orbiter.caos.ch/ExternalLoadBalancer from file gen.go"; DO NOT EDIT.

package external

import (
	"context"
	"fmt"

	"github.com/mitchellh/mapstructure"

	"github.com/caos/orbiter/internal/core/operator/orbiter"

	"github.com/caos/orbiter/internal/kinds/loadbalancers/external/adapter"
	"github.com/caos/orbiter/internal/kinds/loadbalancers/external/model"
	v0builder "github.com/caos/orbiter/internal/kinds/loadbalancers/external/model/v0"
)

type Version int

const (
	unknown Version = iota
	v0
)

type assembler struct {
	path      []string
	overwrite func(map[string]interface{})
	builder   adapter.Builder
	built     adapter.Adapter
}

func New(configPath []string, overwrite func(map[string]interface{}), builder adapter.Builder) orbiter.Assembler {
	return &assembler{configPath, overwrite, builder, nil}
}

func (a *assembler) String() string { return "orbiter.caos.ch/ExternalLoadBalancer" }
func (a *assembler) BuildContext() ([]string, func(map[string]interface{})) {
	return a.path, a.overwrite
}
func (a *assembler) Ensure(ctx context.Context, secrets *orbiter.Secrets, ensuredDependencies map[string]interface{}) (interface{}, error) {
	return a.built.Ensure(ctx, secrets, ensuredDependencies)
}
func (a *assembler) Build(serialized map[string]interface{}, nodeagentupdater orbiter.NodeAgentUpdater, secrets *orbiter.Secrets, dependant interface{}) (orbiter.Kind, interface{}, []orbiter.Assembler, string, error) {

	kind := orbiter.Kind{}
	if err := mapstructure.Decode(serialized, &kind); err != nil {
		return kind, nil, nil, model.CurrentVersion, err
	}

	if kind.Kind != "orbiter.caos.ch/ExternalLoadBalancer" {
		return kind, nil, nil, model.CurrentVersion, fmt.Errorf("Kind %s must be \"orbiter.caos.ch/ExternalLoadBalancer\"", kind.Kind)
	}

	var spec model.UserSpec
	var subassemblersBuilder func(model.Config) ([]orbiter.Assembler, error)
	switch kind.Version {
	case v0.String():
		spec, subassemblersBuilder = v0builder.Build(serialized, secrets, dependant)
	default:
		return kind, nil, nil, model.CurrentVersion, fmt.Errorf("Unknown version %s", kind.Version)
	}

	cfg, adapter, err := a.builder.Build(spec, nodeagentupdater)
	if err != nil {
		return kind, nil, nil, model.CurrentVersion, err
	}
	a.built = adapter

	if subassemblersBuilder == nil {
		return kind, cfg, nil, model.CurrentVersion, nil
	}

	subassemblers, err := subassemblersBuilder(cfg)
	if err != nil {
		return kind, nil, nil, model.CurrentVersion, err
	}

	return kind, cfg, subassemblers, model.CurrentVersion, nil
}
