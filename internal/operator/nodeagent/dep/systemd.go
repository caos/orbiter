package dep

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"

	"github.com/caos/orbos/mntr"
)

type SystemD struct {
	ctx     context.Context
	monitor mntr.Monitor
}

func NewSystemD(ctx context.Context, monitor mntr.Monitor) *SystemD {
	return &SystemD{ctx, monitor}
}

func (s *SystemD) Disable(binary string) error {

	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	cmd := exec.CommandContext(s.ctx, "systemctl", "stop", binary)
	cmd.Stderr = errBuf
	if s.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	err := cmd.Run()
	if err != nil {
		errString := errBuf.String()
		if strings.Contains(errString, "not loaded") {
			err = nil
		} else {
			return errors.Wrapf(err, "stopping %s by systemd failed with stderr %s", binary, errString)
		}
	}

	errBuf.Reset()
	cmd = exec.CommandContext(s.ctx, "systemctl", "disable", binary)
	cmd.Stderr = errBuf
	if s.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		errString := errBuf.String()
		if strings.Contains(errString, "No such file or directory") {
			err = nil
		} else {
			return errors.Wrapf(err, "disabling %s by systemd failed with stderr %s", binary, errString)
		}
	}

	return nil
}

func (s *SystemD) Start(binary string) error {
	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	cmd := exec.CommandContext(s.ctx, "systemctl", "restart", binary)
	cmd.Stderr = errBuf
	return errors.Wrapf(cmd.Run(), "restarting %s from systemd failed with stderr %s", binary, errBuf.String())
}

func (s *SystemD) Enable(binary string) error {

	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	cmd := exec.CommandContext(s.ctx, "systemctl", "enable", binary)
	cmd.Stderr = errBuf
	if s.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}

	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "enabling systemd unit %s failed with stderr %s", binary, errBuf.String())
	}

	if !s.Active(binary) {
		return s.Start(binary)
	}
	return nil
}

func (s *SystemD) Active(binary string) bool {
	cmd := exec.CommandContext(s.ctx, "systemctl", "is-active", binary)
	if s.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	return cmd.Run() == nil
}
