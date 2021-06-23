package sysctl

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
	"github.com/caos/orbos/mntr"
)

type Installer interface {
	isSysctl()
	nodeagent.Installer
}
type sysctlDep struct {
	monitor mntr.Monitor
}

func New(monitor mntr.Monitor) Installer {
	return &sysctlDep{monitor: monitor}
}

func (sysctlDep) isSysctl() {}

func (sysctlDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (sysctlDep) String() string { return "sysctl" }

func (*sysctlDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*sysctlDep)
	return ok
}

var supportedModules = []common.KernelModule{common.IpForward, common.NonLocalBind, common.BridgeNfCallIptables, common.BridgeNfCallIp6tables}

func Contains(this common.Package, that common.Package) bool {
	if that.Config == nil {
		return true
	}

	for thatKey, thatValue := range that.Config {
		if this.Config == nil {
			return false
		}
		if thisValue, ok := this.Config[thatKey]; !ok || thatValue == "1" && thisValue != "1" {
			return false
		}
	}
	return true
}

func Enable(pkg *common.Package, property common.KernelModule) {
	if pkg.Config == nil {
		pkg.Config = make(map[string]string)
	}

	for idx := range supportedModules {
		module := supportedModules[idx]
		if _, ok := pkg.Config[string(module)]; !ok {
			pkg.Config[string(module)] = "0"
		}
	}
	pkg.Config[string(property)] = "1"
}

func (s *sysctlDep) Current() (pkg common.Package, err error) {

	for idx := range supportedModules {
		module := supportedModules[idx]
		if err := currentSysctlConfig(s.monitor, module, &pkg); err != nil {
			return pkg, err
		}
	}

	return pkg, nil
}

func (s *sysctlDep) Ensure(_ common.Package, ensure common.Package, _ bool) error {

	if err := ioutil.WriteFile("/etc/sysctl.d/90-orbiter.conf", []byte(fmt.Sprintf(
		`%s = %s
%s = %s
%s = %s
%s = %s
`,
		string(common.IpForward), oneOrZero(ensure.Config, common.IpForward),
		string(common.NonLocalBind), oneOrZero(ensure.Config, common.NonLocalBind),
		string(common.BridgeNfCallIptables), oneOrZero(ensure.Config, common.BridgeNfCallIptables),
		string(common.BridgeNfCallIp6tables), oneOrZero(ensure.Config, common.BridgeNfCallIp6tables),
	)), os.ModePerm); err != nil {
		return err
	}

	cmd := exec.Command("sysctl", "--system")
	if output, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(err, "running %s failed with stderr %s", strings.Join(cmd.Args, " "), string(output))
	}
	return nil
}

func oneOrZero(cfg map[string]string, property common.KernelModule) string {
	val := cfg[string(property)]
	if val == "1" {
		return val
	}
	return "0"
}

func currentSysctlConfig(monitor mntr.Monitor, property common.KernelModule, pkg *common.Package) error {

	propertyStr := string(property)

	outBuf := new(bytes.Buffer)
	defer outBuf.Reset()
	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	cmd := exec.Command("sysctl", propertyStr)
	cmd.Stderr = errBuf
	cmd.Stdout = outBuf

	fullCmd := strings.Join(cmd.Args, " ")
	monitor.WithFields(map[string]interface{}{"cmd": fullCmd}).Debug("Executing")

	if err := cmd.Run(); err != nil {
		errStr := errBuf.String()
		if !strings.Contains(errStr, "No such file or directory") {
			return errors.Wrapf(err, "running %s failed with stderr %s", fullCmd, errStr)
		}
	}

	if pkg.Config == nil {
		pkg.Config = make(map[string]string)
	}
	pkg.Config[propertyStr] = "0"
	enabled := outBuf.String() == fmt.Sprintf("%s = 1\n", property)
	if enabled {
		pkg.Config[propertyStr] = "1"
	}

	return nil
}
