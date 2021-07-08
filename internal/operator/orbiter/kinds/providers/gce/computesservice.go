package gce

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/caos/orbos/mntr"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/ssh"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/core"

	uuid "github.com/satori/go.uuid"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"google.golang.org/api/compute/v1"
)

var _ core.MachinesService = (*machinesService)(nil)

type creatingInstance struct {
	zone string
	id   string
}

type machinesService struct {
	context *context
	oneoff  bool
	key     *SSHKey
	cache   struct {
		instances         map[string][]*instance
		creatingInstances map[string][]*creatingInstance
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
	desired, ok := m.context.desired.Pools[poolName]
	if !ok {
		return 0
	}

	if desired.Multizonal != nil && len(desired.Multizonal) > 0 {
		return (len(desired.Multizonal) * instances)
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

func (m *machinesService) restartPreemptibleMachines() error {
	pools, err := getAllInstances(m)
	if err != nil {
		return err
	}

	for _, pool := range pools {
		for _, instance := range pool {
			if instance.start {
				if err := operateFunc(
					func() { instance.Monitor.Debug("Restarting preemptible instance") },
					computeOpCall(m.context.client.Instances.Start(m.context.projectID, instance.zone, instance.ID()).RequestId(uuid.NewV1().String()).Do),
					func() error { instance.Monitor.Info("Preemptible instance restarted"); return nil },
				)(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func getDesiredZones(defaultZone string, multizonal []string) []string {
	zones := make([]string, 0)
	if multizonal != nil && len(multizonal) > 0 {
		zones = multizonal
	} else {
		zones = append(zones, defaultZone)
	}
	return zones
}

func (m *machinesService) Create(poolName string, desiredInstances int) (infra.Machines, error) {
	desired, ok := m.context.desired.Pools[poolName]
	if !ok {
		return nil, fmt.Errorf("Pool %s is not configured", poolName)
	}
	usableZone := m.context.desired.Zone
	zones := getDesiredZones(m.context.desired.Zone, desired.Multizonal)

	// Calculate minimum cpu and memory according to the gce specs:
	// https://cloud.google.com/machine/docs/instances/creating-instance-with-custom-machine-type#specifications
	cores := desired.MinCPUCores
	if cores > 1 {
		if cores%2 != 0 {
			cores++
		}
	}
	memory := float64(desired.MinMemoryGB * 1024)
	memoryPerCore := memory / float64(cores)
	minMemPerCore := 922
	maxMemPerCore := 6656
	for memoryPerCore < float64(minMemPerCore) {
		memoryPerCore = memory / float64(cores)
		memory += 256
	}

	for memoryPerCore > float64(maxMemPerCore) {
		cores++
		memoryPerCore = float64(memory) / float64(cores)
	}

	infraMachines := make([]infra.Machine, 0)
	currentInfraMachines := make([]infra.Machine, 0)
	if len(zones) > 1 {
		currentInfraMachinesT, err := m.List(poolName)
		if err != nil {
			return nil, err
		}
		currentInfraMachines = currentInfraMachinesT
		usableZone = ""
		for zoneI := range zones {
			zone := zones[zoneI]
			zoneCovered := 0
			for _, currentInfraMachine := range currentInfraMachines {
				currentGCEMachine, ok := currentInfraMachine.(machine)
				replaceRequired := false
				for _, replaceRequiredID := range m.context.desired.ReplacementRequired {
					if currentInfraMachine.ID() == replaceRequiredID {
						replaceRequired = true
					}
				}
				if ok && zone == currentGCEMachine.Zone() && !replaceRequired {
					zoneCovered++
				}
			}
			for _, currentCreating := range m.cache.creatingInstances[poolName] {
				if currentCreating.zone == zone {
					zoneCovered++
				}
			}
			if zoneCovered >= desiredInstances {
				continue
			}
			// find first usable zone to add machine to then leave loop
			usableZone = zone
			break
		}

		if usableZone == "" {
			return nil, errors.New("error while creating all zones already covered")
		}
	}

	name := newName()
	if m.cache.creatingInstances == nil {
		m.cache.creatingInstances = map[string][]*creatingInstance{}
	}
	if m.cache.creatingInstances[poolName] == nil {
		m.cache.creatingInstances[poolName] = make([]*creatingInstance, 0)
	}
	m.cache.creatingInstances[poolName] = append(m.cache.creatingInstances[poolName], &creatingInstance{
		zone: usableZone,
		id:   name,
	})

	monitor := m.context.monitor.WithFields(map[string]interface{}{
		"machine": name,
		"pool":    poolName,
	})
	infraMachine, err := m.getCreatableMachine(
		monitor,
		poolName,
		desired,
		name,
		usableZone,
		cores,
		memory,
	)
	if err != nil {
		return nil, err
	}

	if m.cache.instances != nil {
		if _, ok := m.cache.instances[poolName]; !ok {
			m.cache.instances[poolName] = make([]*instance, 0)
		}
		m.cache.instances[poolName] = append(m.cache.instances[poolName], infraMachine)
	}

	if err := m.onCreate(poolName, infraMachine); err != nil {
		return nil, err
	}
	monitor.Info("Machine created")
	infraMachines = append(infraMachines, infraMachine)
	for i, instance := range m.cache.creatingInstances[poolName] {
		if instance.id == infraMachine.ID() {
			m.cache.creatingInstances[poolName] = append(m.cache.creatingInstances[poolName][:i], m.cache.creatingInstances[poolName][i+1:]...)
		}
	}
	return infraMachines, nil
}

func (m *machinesService) getCreatableMachine(monitor mntr.Monitor, poolName string, desired *Pool, name string, zone string, cores int, memory float64) (*instance, error) {
	disks := []*compute.AttachedDisk{{
		Type:       "PERSISTENT",
		AutoDelete: true,
		Boot:       true,
		InitializeParams: &compute.AttachedDiskInitializeParams{
			DiskSizeGb:  int64(desired.StorageGB),
			SourceImage: desired.OSImage,
			DiskType:    fmt.Sprintf("zones/%s/diskTypes/%s", zone, desired.StorageDiskType),
		}},
	}

	diskNames := make([]string, desired.LocalSSDs)
	for i := 0; i < int(desired.LocalSSDs); i++ {
		name := fmt.Sprintf("nvme0n%d", i+1)
		disks = append(disks, &compute.AttachedDisk{
			Type:       "SCRATCH",
			AutoDelete: true,
			Boot:       false,
			Interface:  "NVME",
			InitializeParams: &compute.AttachedDiskInitializeParams{
				DiskType: fmt.Sprintf("zones/%s/diskTypes/local-ssd", zone),
			},
			DeviceName: name,
		})
		diskNames[i] = name
	}

	nwTags := networkTags(m.context.orbID, m.context.providerID, poolName)
	sshKey := fmt.Sprintf("orbiter:%s", m.key.Public.Value)
	createInstance := &compute.Instance{
		Name:        name,
		MachineType: fmt.Sprintf("zones/%s/machineTypes/custom-%d-%d", zone, cores, int(memory)),
		Tags:        &compute.Tags{Items: nwTags},
		NetworkInterfaces: []*compute.NetworkInterface{{
			Network: m.context.networkURL,
		}},
		Labels:     map[string]string{"orb": m.context.orbID, "provider": m.context.providerID, "pool": poolName},
		Disks:      disks,
		Scheduling: &compute.Scheduling{Preemptible: desired.Preemptible},
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{{
				Key:   "ssh-keys",
				Value: &sshKey,
			}},
		},
		ServiceAccounts: []*compute.ServiceAccount{{
			Scopes: []string{"https://www.googleapis.com/auth/compute"},
		}},
	}

	if err := operateFunc(
		func() { monitor.Debug("Creating instance") },
		computeOpCall(m.context.client.Instances.Insert(m.context.projectID, zone, createInstance).RequestId(uuid.NewV1().String()).Do),
		func() error { monitor.Info("Instance created"); return nil },
	)(); err != nil {
		return nil, err
	}

	newInstance, err := m.context.client.Instances.Get(m.context.projectID, zone, createInstance.Name).
		Fields("selfLink,networkInterfaces(networkIP)").
		Do()
	if err != nil {
		return nil, err
	}

	var machine machine
	if m.oneoff {
		machine = newGCEMachine(m.context, monitor, createInstance.Name, zone)
	} else {
		sshMachine := ssh.NewMachine(monitor, "orbiter", newInstance.NetworkInterfaces[0].NetworkIP)
		if err := sshMachine.UseKey([]byte(m.key.Private.Value)); err != nil {
			return nil, err
		}
		machine = sshMachine
	}

	infraMachine := newMachine(
		m.context,
		monitor,
		createInstance.Name,
		newInstance.NetworkInterfaces[0].NetworkIP,
		newInstance.SelfLink,
		poolName,
		zone,
		m.removeMachineFunc(
			poolName,
			createInstance.Name,
			zone,
		),
		false,
		machine,
		false,
		func() {},
		func() {},
		false,
		func() {},
		func() {},
	)

	for idx, name := range diskNames {
		mountPoint := fmt.Sprintf("/mnt/disks/%s", name)
		if err := infra.Try(monitor, time.NewTimer(time.Minute), 10*time.Second, infraMachine, func(m infra.Machine) error {
			_, formatErr := m.Execute(nil,
				fmt.Sprintf("sudo mkfs.ext4 -F /dev/%s && sudo mkdir -p /mnt/disks/%s && sudo mount -o discard,defaults,nobarrier /dev/%s %s && sudo chmod a+w %s && echo UUID=`sudo blkid -s UUID -o value /dev/disk/by-id/google-local-nvme-ssd-%d` %s ext4 discard,defaults,nofail,nobarrier 0 2 | sudo tee -a /etc/fstab", name, name, name, mountPoint, mountPoint, idx, mountPoint),
			)
			return formatErr
		}); err != nil {
			remove, cleanupErr := infraMachine.Destroy()
			if cleanupErr != nil {
				panic(cleanupErr)
			}
			if rmErr := remove(); rmErr != nil {
				panic(rmErr)
			}
			return nil, err
		}
		monitor.WithField("mountpoint", mountPoint).Info("Disk formatted")
	}

	return infraMachine, nil
}

func (m *machinesService) ListPools() ([]string, error) {
	pools, err := getAllInstances(m)
	if err != nil {
		return nil, err
	}

	var poolNames []string
	for poolName := range pools {
		copyPoolName := poolName
		poolNames = append(poolNames, copyPoolName)
	}
	return poolNames, nil
}

func (m *machinesService) List(poolName string) (infra.Machines, error) {
	pools, err := getAllInstances(m)
	if err != nil {
		return nil, err
	}

	pool := pools[poolName]
	machines := make([]infra.Machine, len(pool))
	for idx, machine := range pool {
		copyInstance := *machine
		machines[idx] = &copyInstance
	}

	return machines, nil
}

func getAllInstances(m *machinesService) (map[string][]*instance, error) {
	if m.cache.instances != nil {
		return m.cache.instances, nil
	} else {
		m.cache.instances = make(map[string][]*instance)
	}

	region, err := m.context.client.Regions.Get(m.context.projectID, m.context.desired.Region).Do()
	if err != nil {
		return nil, err
	}

	for zoneURLI := range region.Zones {
		zoneURL := region.Zones[zoneURLI]
		zoneURLParts := strings.Split(zoneURL, "/")
		zone := zoneURLParts[(len(zoneURLParts) - 1)]

		instances, err := m.context.client.Instances.
			List(m.context.projectID, zone).
			Filter(fmt.Sprintf(`labels.orb=%s AND labels.provider=%s`, m.context.orbID, m.context.providerID)).
			Fields("items(name,labels,selfLink,status,scheduling(preemptible),networkInterfaces(networkIP))").
			Do()
		if err != nil {
			return nil, err
		}

		for _, inst := range instances.Items {

			if inst.Labels["orb"] != m.context.orbID || inst.Labels["provider"] != m.context.providerID {
				continue
			}

			pool := inst.Labels["pool"]

			var machine machine
			if m.oneoff {
				machine = newGCEMachine(m.context, m.context.monitor.WithFields(toFields(inst.Labels)), inst.Name, zone)
			} else {
				sshMachine := ssh.NewMachine(m.context.monitor.WithFields(toFields(inst.Labels)), "orbiter", inst.NetworkInterfaces[0].NetworkIP)
				if err := sshMachine.UseKey([]byte(m.key.Private.Value)); err != nil {
					return nil, err
				}
				machine = sshMachine
			}

			rebootRequired := false
			unrequireReboot := func() {}
			for idx, req := range m.context.desired.RebootRequired {
				if req == inst.Name {
					rebootRequired = true
					unrequireReboot = func(pos int) func() {
						return func() {
							copy(m.context.desired.RebootRequired[pos:], m.context.desired.RebootRequired[pos+1:])
							m.context.desired.RebootRequired[len(m.context.desired.RebootRequired)-1] = ""
							m.context.desired.RebootRequired = m.context.desired.RebootRequired[:len(m.context.desired.RebootRequired)-1]
						}
					}(idx)
					break
				}
			}

			replacementRequired := false
			unrequireReplacement := func() {}
			for idx, req := range m.context.desired.ReplacementRequired {
				if req == inst.Name {
					replacementRequired = true
					unrequireReplacement = func(pos int) func() {
						return func() {
							copy(m.context.desired.ReplacementRequired[pos:], m.context.desired.ReplacementRequired[pos+1:])
							m.context.desired.ReplacementRequired[len(m.context.desired.ReplacementRequired)-1] = ""
							m.context.desired.ReplacementRequired = m.context.desired.ReplacementRequired[:len(m.context.desired.ReplacementRequired)-1]
						}
					}(idx)
					break
				}
			}

			mach := newMachine(
				m.context,
				m.context.monitor.WithField("name", inst.Name).WithFields(toFields(inst.Labels)),
				inst.Name,
				inst.NetworkInterfaces[0].NetworkIP,
				inst.SelfLink,
				pool,
				zone,
				m.removeMachineFunc(pool, inst.Name, zone),
				inst.Status == "TERMINATED" && inst.Scheduling.Preemptible,
				machine,
				rebootRequired,
				func(id string) func() {
					return func() { m.context.desired.RebootRequired = append(m.context.desired.RebootRequired, id) }
				}(inst.Name),
				unrequireReboot,
				replacementRequired,
				func(id string) func() {
					return func() { m.context.desired.ReplacementRequired = append(m.context.desired.ReplacementRequired, id) }
				}(inst.Name),
				unrequireReplacement,
			)

			m.cache.instances[pool] = append(m.cache.instances[pool], mach)
		}
	}

	return m.cache.instances, nil
}

func toFields(labels map[string]string) map[string]interface{} {
	fields := make(map[string]interface{})
	for key, label := range labels {
		fields[key] = label
	}
	return fields
}

func (m *machinesService) removeMachineFunc(pool, id, zone string) func() error {
	return func() error {

		m.cache.Lock()
		cleanMachines := make([]*instance, 0)
		for _, cachedMachine := range m.cache.instances[pool] {
			if cachedMachine.X_ID != id {
				cleanMachines = append(cleanMachines, cachedMachine)
			}
		}
		m.cache.instances[pool] = cleanMachines
		m.cache.Unlock()

		return removeResourceFunc(
			m.context.monitor.WithFields(map[string]interface{}{
				"pool":    pool,
				"machine": id,
			}),
			"instance",
			id,
			m.context.client.Instances.Delete(m.context.projectID, zone, id).RequestId(uuid.NewV1().String()).Do,
		)()
	}
}

func networkTags(orbID, providerID string, poolName ...string) []string {
	tags := []string{
		orbNetworkTag(orbID),
		fmt.Sprintf("provider-%s", providerID),
	}
	for _, pool := range poolName {
		tags = append(tags, fmt.Sprintf("pool-%s", pool))
	}
	return tags
}

func orbNetworkTag(orbID string) string {
	return fmt.Sprintf("orb-%s", orbID)
}
