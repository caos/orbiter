package kubernetes

import (
	"fmt"

	"github.com/caos/orbos/internal/secret"
	"github.com/caos/orbos/internal/tree"
	"github.com/pkg/errors"
	core "k8s.io/api/core/v1"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/orbiter"
)

var ipPartRegex = `([0-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5])`

var ipRegex = fmt.Sprintf(`%s\.%s\.%s\.%s`, ipPartRegex, ipPartRegex, ipPartRegex, ipPartRegex)

var cidrRegex = fmt.Sprintf(`%s/([1-2][0-9]|3[0-2]|[0-9])`, ipRegex)

type DesiredV0 struct {
	Common tree.Common `yaml:",inline"`
	Spec   Spec
}

type Spec struct {
	ControlPlane Pool
	Kubeconfig   *secret.Secret `yaml:",omitempty"`
	Networking   struct {
		DNSDomain         string
		Network           string
		ServiceCidr       orbiter.CIDR
		PodCidr           orbiter.CIDR
		OpenFirewallPorts []*common.Allowed
	}
	Verbose  bool
	Versions struct {
		Kubernetes string
		Orbiter    string
	}
	Workers []*Pool
}

func parseDesiredV0(desiredTree *tree.Tree) (*DesiredV0, error) {
	desiredKind := &DesiredV0{
		Common: *desiredTree.Common,
		Spec:   Spec{},
	}
	if err := desiredTree.Original.Decode(desiredKind); err != nil {
		return nil, errors.Wrap(err, "parsing desired state failed")
	}

	return desiredKind, nil
}

func (d *DesiredV0) validate() error {

	if d.Spec.ControlPlane.Nodes != 1 && d.Spec.ControlPlane.Nodes != 3 && d.Spec.ControlPlane.Nodes != 5 {
		return errors.Errorf("Controlplane nodes can only be scaled to 1, 3 or 5 but desired are %d", d.Spec.ControlPlane.Nodes)
	}

	if ParseString(d.Spec.Versions.Kubernetes) == Unknown {
		return errors.Errorf("Unknown kubernetes version %s", d.Spec.Versions.Kubernetes)
	}

	if d.Spec.Networking.Network != "cilium" && d.Spec.Networking.Network != "calico" && d.Spec.Networking.Network != "flannel" {
		return errors.Errorf("Network must eighter be calico, flannel or cilium, but got %s", d.Spec.Networking.Network)
	}

	if err := d.Spec.Networking.ServiceCidr.Validate(); err != nil {
		return err
	}

	if err := d.Spec.Networking.PodCidr.Validate(); err != nil {
		return err
	}

	for _, port := range d.Spec.Networking.OpenFirewallPorts {
		if err := port.Validate(); err != nil {
			return err
		}
	}

	seenPools := map[string][]string{
		d.Spec.ControlPlane.Provider: []string{d.Spec.ControlPlane.Pool},
	}

	for _, worker := range d.Spec.Workers {
		pools, ok := seenPools[worker.Provider]
		if !ok {
			seenPools[worker.Provider] = []string{worker.Pool}
			continue
		}
		for _, seenPool := range pools {
			if seenPool == worker.Pool {
				return errors.Errorf("Pool %s from provider %s is used multiple times", worker.Pool, worker.Provider)
			}
		}
		seenPools[worker.Provider] = append(pools, worker.Pool)
	}

	return nil
}

type Pool struct {
	UpdatesDisabled bool
	Provider        string
	Nodes           int
	Pool            string
	Taints          *Taints `yaml:"taints,omitempty"`
}

type Taint struct {
	Key    string           `yaml:"key"`
	Value  string           `yaml:"value,omitempty"`
	Effect core.TaintEffect `yaml:"effect"`
}

type Taints []Taint

func (t *Taints) ToK8sTaints() []core.Taint {
	if t == nil {
		return nil
	}
	taints := make([]core.Taint, len(*t))
	for idx, taint := range *t {
		taints[idx] = core.Taint{
			Key:    taint.Key,
			Value:  taint.Value,
			Effect: taint.Effect,
		}
	}
	return taints
}
