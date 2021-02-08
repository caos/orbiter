package static

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	secret2 "github.com/caos/orbos/pkg/secret"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"
	"github.com/caos/orbos/mntr"
)

var _ core.MachinesService = (*machinesService)(nil)

type machinesService struct {
	ctx        context.Context
	monitor    mntr.Monitor
	desired    *DesiredV0
	statusFile string
	onCreate   func(machine infra.Machine, pool string) error
	cache      map[string]cachedMachines
}

func newMachinesService(
	ctx context.Context,
	monitor mntr.Monitor,
	desired *DesiredV0,
	id string) *machinesService {
	return &machinesService{
		ctx,
		monitor,
		desired,
		filepath.Join("/var/orbiter", id),
		nil,
		nil,
	}
}

func (c *machinesService) updateKeys() error {

	pools, err := c.ListPools()
	if err != nil {
		panic(err)
	}

	keys := privateKeys(c.desired.Spec)

	for _, pool := range pools {
		machines, err := c.cachedPool(pool)
		if err != nil {
			return err
		}
		for _, machine := range machines {
			if err := machine.UseKey(keys...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *machinesService) ListPools() ([]string, error) {

	pools := make([]string, 0)

	for key := range c.desired.Spec.Pools {
		pools = append(pools, key)
	}

	return pools, nil
}

func (c *machinesService) List(poolName string) (infra.Machines, error) {
	pool, err := c.cachedPool(poolName)
	if err != nil {
		return nil, err
	}

	return pool.Machines(), nil
}

func (c *machinesService) Create(poolName string) (infra.Machine, error) {
	pool, err := c.cachedPool(poolName)
	if err != nil {
		return nil, err
	}

	for _, machine := range pool {

		if err := machine.WriteFile(fmt.Sprintf("/home/orbiter/.ssh/authorized_keys"), bytes.NewReader([]byte(c.desired.Spec.Keys.MaintenanceKeyPublic.Value)), 600); err != nil {
			return nil, err
		}

		if !machine.X_active {

			if err := machine.WriteFile(c.statusFile, strings.NewReader("active"), 600); err != nil {
				return nil, err
			}

			if err := c.onCreate(machine, poolName); err != nil {
				return nil, err
			}

			machine.X_active = true
			return machine, nil
		}
	}

	return nil, errors.New("no machines left")
}

func (c *machinesService) cachedPool(poolName string) (cachedMachines, error) {

	specifiedMachines, ok := c.desired.Spec.Pools[poolName]
	if !ok {
		return nil, fmt.Errorf("pool %s does not exist", poolName)
	}

	cache, ok := c.cache[poolName]
	if ok {
		return cache, nil
	}

	keys := privateKeys(c.desired.Spec)

	newCache := make([]*machine, 0)

	initializeMachine := func(rebootRequired bool, replacementRequired bool, spec *Machine) *machine {
		return newMachine(c.ctx, c.monitor, c.statusFile, "orbiter", &spec.ID, string(spec.IP),
			rebootRequired,
			func() {
				spec.RebootRequired = true
			}, func() {
				spec.RebootRequired = false
			},
			replacementRequired,
			func() {
				spec.ReplacementRequired = true
			}, func() {
				spec.ReplacementRequired = false
			})
	}
	for _, spec := range specifiedMachines {

		machine := initializeMachine(spec.RebootRequired, spec.ReplacementRequired, spec)
		if err := machine.UseKey(keys...); err != nil {
			return nil, err
		}

		buf := new(bytes.Buffer)
		if err := machine.ReadFile(c.statusFile, buf); err != nil {
			// if error, treat as active
		}
		machine.X_active = strings.Contains(buf.String(), "active")
		buf.Reset()
		newCache = append(newCache, machine)
	}

	if c.cache == nil {
		c.cache = make(map[string]cachedMachines)
	}
	c.cache[poolName] = newCache
	return newCache, nil
}

type cachedMachines []*machine

func (c cachedMachines) Machines() infra.Machines {
	machines := make([]infra.Machine, 0)
	for _, machine := range c {
		if machine.X_active {
			machines = append(machines, machine)
		}
	}
	return machines
}

func privateKeys(spec Spec) [][]byte {
	var privateKeys [][]byte
	toBytes := func(key *secret2.Secret) {
		if key != nil {
			privateKeys = append(privateKeys, []byte(key.Value))
		}
	}
	toBytes(spec.Keys.BootstrapKeyPrivate)
	toBytes(spec.Keys.MaintenanceKeyPrivate)
	return privateKeys
}
