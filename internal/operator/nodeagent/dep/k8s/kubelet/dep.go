package kubelet

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/k8s"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/selinux"
	"github.com/caos/orbos/mntr"
)

type Installer interface {
	isKubelet()
	nodeagent.Installer
}

type kubeletDep struct {
	ctx     context.Context
	os      dep.OperatingSystem
	monitor mntr.Monitor
	common  *k8s.Common
	systemd *dep.SystemD
}

func New(ctx context.Context, monitor mntr.Monitor, os dep.OperatingSystem, manager *dep.PackageManager, systemd *dep.SystemD) Installer {
	return &kubeletDep{ctx, os, monitor, k8s.New(os, manager, "kubelet"), systemd}
}

func (kubeletDep) isKubelet() {}

func (kubeletDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (k kubeletDep) String() string { return "Kubelet" }

func (*kubeletDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*kubeletDep)
	return ok
}

func (k *kubeletDep) Current() (pkg common.Package, err error) {

	pkg, err = k.common.Current()
	if err != nil {
		return pkg, err
	}

	return pkg, selinux.Current(k.os, &pkg)
}

func (k *kubeletDep) Ensure(remove common.Package, install common.Package) error {

	if err := selinux.EnsurePermissive(k.monitor, k.os, remove); err != nil {
		return err
	}

	if k.os != dep.CentOS {
		return k.ensurePackage(remove, install)
	}

	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	cmd := exec.CommandContext(k.ctx, "modprobe", "br_netfilter")
	cmd.Stderr = errBuf
	if k.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("loading module br_netfilter while installing kubelet failed with stderr %s: %w", errBuf.String(), err)
	}
	errBuf.Reset()

	return k.ensurePackage(remove, install)
}

func (k *kubeletDep) ensurePackage(remove common.Package, install common.Package) error {

	if err := k.common.Ensure(remove, install); err != nil {
		return err
	}

	if err := k.systemd.Enable("kubelet"); err != nil {
		return err
	}

	return k.systemd.Start("kubelet")
}
