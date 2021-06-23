package sshd

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
)

type Installer interface {
	isSSHD()
	nodeagent.Installer
}

type sshdDep struct {
	systemd *dep.SystemD
}

func New(systemd *dep.SystemD) Installer {
	return &sshdDep{systemd}
}

func (sshdDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (sshdDep) isSSHD() {}

func (sshdDep) String() string { return "SSHD" }

func (*sshdDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*sshdDep)
	return ok
}

func (s *sshdDep) Current() (pkg common.Package, err error) {

	buf := new(bytes.Buffer)
	defer buf.Reset()

	swapon := exec.Command("sshd", "-T")
	swapon.Stdout = buf
	if err := swapon.Run(); err != nil {
		return pkg, err
	}

	for {
		if err != nil && err != io.EOF {
			return pkg, err
		}
		line, err := buf.ReadString('\n')
		fields := strings.Fields(line)
		value := ""
		if len(fields) > 1 {
			value = fields[1]
		}

		if strings.Contains(line, "listenaddress") {
			checkIP := "127.0.0.1"
			if value != "[::]:22" && value != "0.0.0.0:22" {
				if pkg.Config == nil {
					pkg.Config = make(map[string]string)
				}
				checkIP = strings.Split(value, ":")[0]
				pkg.Config["listenaddress"] = checkIP
			}
			out, _ := exec.Command("ssh", "-T", checkIP).CombinedOutput()
			if strings.Contains(string(out), "Connection refused") {
				if pkg.Config == nil {
					pkg.Config = make(map[string]string)
				}
				pkg.Config["listening"] = "false"
			}
		}

		if strings.Contains(line, "gssapiauthentication") && value != "no" {
			if pkg.Config == nil {
				pkg.Config = make(map[string]string)
			}
			pkg.Config["gssapiauthentication"] = value
		}

		if err == io.EOF {
			break
		}
	}
	return pkg, nil
}

func (s *sshdDep) Ensure(remove common.Package, ensure common.Package, _ bool) error {

	appendLines := []string{"GSSAPIAuthentication no"}
	listenAddress := ensure.Config["listenaddress"]
	if listenAddress != "" {
		appendLines = append(appendLines, fmt.Sprintf("ListenAddress %s", listenAddress))
	}

	if err := dep.ManipulateFile("/etc/ssh/sshd_config", []string{"GSSAPIAuthentication"}, appendLines, func(line string) *string {
		if strings.HasPrefix(line, "ListenAddress") {
			return nil
		}
		return &line
	}); err != nil {
		return err
	}

	return s.systemd.Start("sshd")
}
