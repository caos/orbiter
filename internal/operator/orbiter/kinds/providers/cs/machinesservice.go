package cs

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"

	"github.com/caos/orbos/internal/helpers"

	"github.com/cloudscale-ch/cloudscale-go-sdk"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/ssh"
	sshgen "github.com/caos/orbos/internal/ssh"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
)

func ListMachines(monitor mntr.Monitor, desiredTree *tree.Tree, orbID, providerID string) (map[string]infra.Machine, error) {
	desired, err := parseDesired(desiredTree)
	if err != nil {
		return nil, errors.Wrap(err, "parsing desired state failed")
	}
	desiredTree.Parsed = desired

	ctx, err := buildContext(monitor, &desired.Spec, orbID, providerID, true)
	if err != nil {
		return nil, err
	}

	if err := ctx.machinesService.use(desired.Spec.SSHKey); err != nil {
		invalidKey := &secret.Secret{Value: "invalid"}
		if err := ctx.machinesService.use(&SSHKey{
			Private: invalidKey,
			Public:  invalidKey,
		}); err != nil {
			panic(err)
		}
	}

	return core.ListMachines(ctx.machinesService)
}

var _ core.MachinesService = (*machinesService)(nil)

type machinesService struct {
	context *context
	oneoff  bool
	key     *SSHKey
	cache   struct {
		instances map[string][]*machine
		sync.Mutex
	}
	onCreate func(pool string, machine infra.Machine) error
}

func newMachinesService(context *context, oneoff bool) *machinesService {
	return &machinesService{
		context: context,
		oneoff:  oneoff,
	}
}

func (m *machinesService) DesiredMachines(poolName string, instances int) int {
	_, ok := m.context.desired.Pools[poolName]
	if !ok {
		return 0
	}

	return instances
}

func (m *machinesService) use(key *SSHKey) error {
	if key == nil || key.Private == nil || key.Public == nil || key.Private.Value == "" || key.Public.Value == "" {
		return errors.New("machines are not connectable. have you configured the orb by running orbctl configure?")
	}
	m.key = key
	return nil
}

func (m *machinesService) Create(poolName string, _ int) (infra.Machines, error) {

	desired, ok := m.context.desired.Pools[poolName]
	if !ok {
		return nil, fmt.Errorf("Pool %s is not configured", poolName)
	}

	name := newName()
	monitor := machineMonitor(m.context.monitor, name, poolName)

	monitor.Debug("Creating instance")

	userData, err := NewCloudinit().AddGroupWithoutUsers(
		"orbiter",
	).AddUser(
		"orbiter",
		true,
		"",
		[]string{"orbiter", "wheel"},
		"orbiter",
		[]string{m.context.desired.SSHKey.Public.Value},
		"ALL=(ALL) NOPASSWD:ALL",
	).AddCmd(
		"sudo echo \"\n\nPermitRootLogin no\n\" >> /etc/ssh/sshd_config",
	).AddCmd(
		"sudo service sshd restart",
	).ToYamlString()
	if err != nil {
		return nil, err
	}

	_, pub, err := sshgen.Generate()
	if err != nil {
		return nil, err
	}

	newServer, err := m.context.client.Servers.Create(m.context.ctx, &cloudscale.ServerRequest{
		ZonalResourceRequest: cloudscale.ZonalResourceRequest{},
		TaggedResourceRequest: cloudscale.TaggedResourceRequest{
			Tags: map[string]string{
				"orb":      m.context.orbID,
				"provider": m.context.providerID,
				"pool":     poolName,
			},
		},
		Name:              name,
		Flavor:            desired.Flavor,
		Image:             "centos-7",
		Zone:              desired.Zone,
		VolumeSizeGB:      desired.VolumeSizeGB,
		Volumes:           nil,
		Interfaces:        nil,
		BulkVolumeSizeGB:  0,
		SSHKeys:           []string{pub},
		Password:          "",
		UsePublicNetwork:  boolPtr(m.oneoff || true), // Always use public Network
		UsePrivateNetwork: boolPtr(true),
		UseIPV6:           boolPtr(false),
		AntiAffinityWith:  "",
		ServerGroups:      nil,
		UserData:          userData,
	})
	if err != nil {
		return nil, err
	}

	monitor.Info("Instance created")

	infraMachine, err := m.toMachine(newServer, monitor, desired, poolName)
	if err != nil {
		return nil, err
	}

	if m.cache.instances != nil {
		if _, ok := m.cache.instances[poolName]; !ok {
			m.cache.instances[poolName] = make([]*machine, 0)
		}
		m.cache.instances[poolName] = append(m.cache.instances[poolName], infraMachine)
	}

	if err := m.onCreate(poolName, infraMachine); err != nil {
		return nil, err
	}

	monitor.Info("Machine created")
	return []infra.Machine{infraMachine}, nil
}

func (m *machinesService) toMachine(server *cloudscale.Server, monitor mntr.Monitor, pool *Pool, poolName string) (*machine, error) {
	internalIP, sshIP := createdIPs(server.Interfaces, m.oneoff || true /* always use public ip */)

	sshMachine := ssh.NewMachine(monitor, "orbiter", sshIP)
	if err := sshMachine.UseKey([]byte(m.key.Private.Value)); err != nil {
		return nil, err
	}

	infraMachine := newMachine(
		server,
		internalIP,
		sshIP,
		sshMachine,
		m.removeMachineFunc(server.Tags["pool"], server.UUID),
		m.context,
		pool,
		poolName,
	)
	return infraMachine, nil
}

func createdIPs(interfaces []cloudscale.Interface, oneoff bool) (string, string) {
	var internalIP string
	var sshIP string
	for idx := range interfaces {
		interf := interfaces[idx]

		if internalIP != "" && sshIP != "" {
			break
		}

		if interf.Type == "private" && len(interf.Addresses) > 0 {
			internalIP = interf.Addresses[0].Address
			if !oneoff {
				sshIP = internalIP
				break
			}
		}
		if oneoff && interf.Type == "public" && len(interf.Addresses) > 0 {
			sshIP = interf.Addresses[0].Address
			continue
		}
	}
	return internalIP, sshIP
}

func (m *machinesService) ListPools() ([]string, error) {

	pools, err := m.machines()
	if err != nil {
		return nil, err
	}

	var poolNames []string
	for poolName := range pools {
		poolNames = append(poolNames, poolName)
	}
	return poolNames, nil
}

func (m *machinesService) List(poolName string) (infra.Machines, error) {
	pools, err := m.machines()
	if err != nil {
		return nil, err
	}

	pool := pools[poolName]
	machines := make([]infra.Machine, len(pool))
	for idx := range pool {
		machine := pool[idx]
		machines[idx] = machine
	}

	return machines, nil
}

func (m *machinesService) machines() (map[string][]*machine, error) {
	if m.cache.instances != nil {
		return m.cache.instances, nil
	}

	// TODO: Doesn't work, all machines get destroyed that belong to the token
	servers, err := m.context.client.Servers.List(m.context.ctx /**/, func(r *http.Request) {
		params := r.URL.Query()
		params["tag:orb"] = []string{m.context.orbID}
		params["tag:provider"] = []string{m.context.providerID}
	})
	if err != nil {
		return nil, err
	}

	if m.cache.instances == nil {
		m.cache.instances = make(map[string][]*machine)
	} else {
		for k := range m.cache.instances {
			m.cache.instances[k] = nil
			delete(m.cache.instances, k)
		}
	}

	for idx := range servers {
		server := servers[idx]
		pool := server.Tags["pool"]
		machine, err := m.toMachine(&server, machineMonitor(m.context.monitor, server.Name, pool), m.context.desired.Pools[pool], pool)
		if err != nil {
			return nil, err
		}
		m.cache.instances[pool] = append(m.cache.instances[pool], machine)
	}

	return m.cache.instances, nil
}

func (m *machinesService) removeMachineFunc(pool, uuid string) func() error {

	return func() error {
		m.cache.Lock()
		cleanMachines := make([]*machine, 0)
		for idx := range m.cache.instances[pool] {
			cachedMachine := m.cache.instances[pool][idx]
			if cachedMachine.server.UUID != uuid {
				cleanMachines = append(cleanMachines, cachedMachine)
			}
		}
		m.cache.instances[pool] = cleanMachines
		m.cache.Unlock()

		return m.context.client.Servers.Delete(m.context.ctx, uuid)
	}
}

func machineMonitor(monitor mntr.Monitor, name string, poolName string) mntr.Monitor {
	return monitor.WithFields(map[string]interface{}{
		"machine": name,
		"pool":    poolName,
	})
}

func boolPtr(b bool) *bool { return &b }

func newName() string {
	return "orbos-" + helpers.RandomStringRunes(6, []rune("abcdefghijklmnopqrstuvwxyz0123456789"))
}
