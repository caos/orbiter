package kubeadm

import (
	"regexp"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/k8s"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
)

type Installer interface {
	isKubeadm()
	nodeagent.Installer
}

type kubeadmDep struct {
	manager    *dep.PackageManager
	os         dep.OperatingSystem
	normalizer *regexp.Regexp
	common     *k8s.Common
}

func New(os dep.OperatingSystem, manager *dep.PackageManager) Installer {
	return &kubeadmDep{manager, os, regexp.MustCompile(`\d+\.\d+\.\d+`), k8s.New(os, manager, "kubeadm")}
}

func (kubeadmDep) isKubeadm() {}

func (kubeadmDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (k kubeadmDep) String() string { return "Kubeadm" }

func (*kubeadmDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*kubeadmDep)
	return ok
}

func (k *kubeadmDep) Current() (common.Package, error) {
	return k.common.Current()
}

func (k *kubeadmDep) Ensure(remove common.Package, install common.Package, leaveOSRepositories bool) error {
	return k.common.Ensure(remove, install, leaveOSRepositories)
}
