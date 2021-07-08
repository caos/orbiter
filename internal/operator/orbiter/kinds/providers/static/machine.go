package static

import (
	"strings"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/loadbalancers"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"

	"github.com/caos/orbos/pkg/tree"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/ssh"
	"github.com/caos/orbos/mntr"
)

var _ infra.Machine = (*machine)(nil)

type machine struct {
	poolFile             string
	rebootRequired       bool
	requireReboot        func()
	unrequireReboot      func()
	replacementRequired  bool
	requireReplacement   func()
	unrequireReplacement func()
	*ssh.Machine
	X_ID     *string `header:"id"`
	X_IP     string  `header:"ip"`
	X_active bool    `header:"active"`
}

func newMachine(
	monitor mntr.Monitor,
	poolFile string,
	remoteUser string,
	id *string,
	ip string,
	rebootRequired bool,
	requireReboot func(),
	unrequireReboot func(),
	replacementRequired bool,
	requireReplacement func(),
	unrequireReplacement func(),
) *machine {
	return &machine{
		X_active:             false,
		poolFile:             poolFile,
		X_ID:                 id,
		X_IP:                 ip,
		Machine:              ssh.NewMachine(monitor, remoteUser, ip),
		rebootRequired:       rebootRequired,
		requireReboot:        requireReboot,
		unrequireReboot:      unrequireReboot,
		replacementRequired:  replacementRequired,
		requireReplacement:   requireReplacement,
		unrequireReplacement: unrequireReplacement,
	}
}

func (c *machine) ID() string {
	return *c.X_ID
}

func (c *machine) IP() string {
	return c.X_IP
}

func (c *machine) Destroy() (func() error, error) {
	c.Execute(nil, "sudo systemctl stop node-agentd keepalived nginx")
	c.Execute(nil, "sudo systemctl disable node-agentd keepalived nginx")
	c.X_active = false
	return func() error {
		c.Execute(nil, "sudo kubeadm reset -f")
		c.Execute(nil, "sudo rm -rf /var/lib/etcd")
		return nil
	}, c.Machine.WriteFile(c.poolFile, strings.NewReader(""), 600)
}

func (c *machine) RebootRequired() (bool, func(), func()) {
	return c.rebootRequired, c.requireReboot, c.unrequireReboot
}

func (c *machine) ReplacementRequired() (bool, func(), func()) {
	return c.replacementRequired, c.requireReplacement, c.unrequireReplacement
}

func ListMachines(monitor mntr.Monitor, desiredTree *tree.Tree, providerID string) (map[string]infra.Machine, error) {
	desired, err := parseDesiredV0(desiredTree)
	if err != nil {
		return nil, errors.Wrap(err, "parsing desired state failed")
	}
	desiredTree.Parsed = desired

	_, _, _, _, _, err = loadbalancers.GetQueryAndDestroyFunc(monitor, nil, desired.Loadbalancing, &tree.Tree{}, nil)
	if err != nil {
		return nil, err
	}

	machinesSvc := NewMachinesService(monitor,
		desired,
		providerID)

	if err := machinesSvc.updateKeys(); err != nil {
		return nil, err
	}

	return core.ListMachines(machinesSvc)
}
