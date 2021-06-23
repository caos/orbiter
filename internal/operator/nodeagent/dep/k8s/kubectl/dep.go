package kubectl

import (
	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/k8s"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
)

type Installer interface {
	isKubectl()
	nodeagent.Installer
}

type kubectlDep struct {
	common *k8s.Common
}

func New(os dep.OperatingSystem, manager *dep.PackageManager) Installer {
	return &kubectlDep{k8s.New(os, manager, "kubectl")}
}

func (kubectlDep) isKubectl() {}

func (kubectlDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (k kubectlDep) String() string { return "Kubectl" }

func (*kubectlDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*kubectlDep)
	return ok
}

func (k *kubectlDep) Current() (common.Package, error) {
	return k.common.Current()
}

func (k *kubectlDep) Ensure(remove common.Package, install common.Package, leaveOSRepositories bool) error {
	return k.common.Ensure(remove, install, leaveOSRepositories)
}
