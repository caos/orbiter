package cs

import (
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/ssh"
	"github.com/cloudscale-ch/cloudscale-go-sdk"
)

var _ infra.Machine = (*machine)(nil)

type action struct {
	required  bool
	require   func()
	unrequire func()
}

type machine struct {
	server *cloudscale.Server
	*ssh.Machine
	remove       func() error
	context      *svcConfig
	reboot       *action
	replacement  *action
	pool         *Pool
	poolName     string
	X_ID         string `header:"id"`
	X_internalIP string `header:"internal ip"`
	X_externalIP string `header:"external ip"`
}

func newMachine(server *cloudscale.Server, internalIP, externalIP string, sshMachine *ssh.Machine, remove func() error, context *svcConfig, pool *Pool, poolName string) *machine {
	return &machine{
		server:       server,
		X_ID:         server.Name,
		X_internalIP: internalIP,
		X_externalIP: externalIP,
		Machine:      sshMachine,
		remove:       remove,
		context:      context,
		pool:         pool,
		poolName:     poolName,
	}
}

func (m *machine) ID() string    { return m.X_ID }
func (m *machine) IP() string    { return m.X_internalIP }
func (m *machine) Remove() error { return m.remove() }

func (m *machine) RebootRequired() (required bool, require func(), unrequire func()) {

	m.reboot = m.initAction(
		m.reboot,
		func() []string { return m.context.desired.RebootRequired },
		func(machines []string) { m.context.desired.RebootRequired = machines })

	return m.reboot.required, m.reboot.require, m.reboot.unrequire
}

func (m *machine) ReplacementRequired() (required bool, require func(), unrequire func()) {

	m.replacement = m.initAction(
		m.replacement,
		func() []string { return m.context.desired.ReplacementRequired },
		func(machines []string) { m.context.desired.ReplacementRequired = machines })

	return m.replacement.required, m.replacement.require, m.replacement.unrequire
}

func (m *machine) initAction(a *action, getSlice func() []string, setSlice func([]string)) *action {
	if a != nil {
		return a
	}

	newAction := &action{
		required:  false,
		unrequire: func() {},
		require: func() {
			s := getSlice()
			s = append(s, m.server.UUID)
			setSlice(s)
		},
	}

	s := getSlice()
	for sIdx := range s {
		req := s[sIdx]
		if req == m.ID() {
			newAction.required = true
			break
		}
	}

	if newAction.required {
		newAction.unrequire = func() {
			s := getSlice()
			for sIdx := range s {
				req := s[sIdx]
				if req == m.ID() {
					s = append(s[0:sIdx], s[sIdx+1:]...)
				}
			}
			setSlice(s)
		}
	}

	return newAction
}
