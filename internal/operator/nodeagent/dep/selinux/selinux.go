package selinux

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/mntr"
)

func Current(os dep.OperatingSystem, pkg *common.Package) (err error) {

	if os != dep.CentOS {
		return nil
	}

	if path, err := exec.LookPath("sestatus"); err != nil || path == "" {
		if pkg.Config == nil {
			pkg.Config = make(map[string]string)
		}
		pkg.Config["selinux"] = "permissive"
		return nil
	}

	buf := new(bytes.Buffer)
	defer buf.Reset()

	cmd := exec.CommandContext("sestatus")
	cmd.Stdout = buf
	if err := cmd.Run(); err != nil {
		return err
	}

	for err == nil {
		line, err := buf.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.Contains(line, "Current mode:") {
			status := strings.TrimSpace(strings.Split(line, ":")[1])
			if status != "permissive" {
				if pkg.Config == nil {
					pkg.Config = make(map[string]string)
				}
				pkg.Config["selinux"] = status
			}
			return nil
		}
	}
	return err
}

func EnsurePermissive(monitor mntr.Monitor, opsys dep.OperatingSystem, remove common.Package) error {

	if opsys != dep.CentOS || remove.Config["selinux"] == "permissive" {
		return nil
	}

	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	cmd := exec.CommandContext("setenforce", "0")
	cmd.Stderr = errBuf
	if monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("disabling SELinux failed with stderr %s: %w", errBuf.String(), err)
	}
	errBuf.Reset()

	cmd = exec.CommandContext("sed", "-i", "s/^SELINUX=enforcing$/SELINUX=permissive/", "/etc/selinux/config")
	cmd.Stderr = errBuf
	if monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("disabling SELinux failed with stderr %s: %w", errBuf.String(), err)
	}
	return nil
}
