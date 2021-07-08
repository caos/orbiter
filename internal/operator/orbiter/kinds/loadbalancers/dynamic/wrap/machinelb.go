package wrap

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
)

type cmpLB struct {
	infra.Machine
	desire func() error
}

func machine(machine infra.Machine, desire func() error) infra.Machine {
	return &cmpLB{
		Machine: machine,
		desire:  desire,
	}
}

func (c *cmpLB) Destroy() (func() error, error) {
	remove, err := c.Machine.Destroy()
	if err != nil {
		return nil, err
	}
	return remove, c.desire()
}
