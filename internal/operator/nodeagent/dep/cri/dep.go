package cri

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
	"github.com/caos/orbos/mntr"
)

const containerdVersion = "1.4.3"

type Installer interface {
	isCRI()
	nodeagent.Installer
}

// TODO: Add support for containerd, cri-o, ...
type criDep struct {
	ctx                       context.Context
	monitor                   mntr.Monitor
	os                        dep.OperatingSystemMajor
	manager                   *dep.PackageManager
	dockerVersionPrunerRegexp *regexp.Regexp
	systemd                   *dep.SystemD
}

// New returns a dependency that implements the kubernetes container runtime interface
func New(ctx context.Context, monitor mntr.Monitor, os dep.OperatingSystemMajor, manager *dep.PackageManager, systemd *dep.SystemD) Installer {
	return &criDep{ctx, monitor, os, manager, regexp.MustCompile(`\d+\.\d+\.\d+`), systemd}
}

func (criDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (c criDep) isCRI() {}

func (c criDep) String() string { return "Container Runtime" }

func (s *criDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*criDep)
	return ok
}

func (c *criDep) Current() (pkg common.Package, err error) {
	if !c.systemd.Active("docker") {
		return pkg, err
	}

	var (
		dockerVersion     string
		containerdVersion string
	)
	for _, installedPkg := range c.manager.CurrentVersions("docker-ce", "containerd.io") {
		switch installedPkg.Package {
		case "docker-ce":
			dockerVersion = fmt.Sprintf("%s %s %s", dockerVersion, installedPkg.Package, "v"+c.dockerVersionPrunerRegexp.FindString(installedPkg.Version))
			continue
		case "containerd.io":
			containerdVersion = installedPkg.Version
			continue
		default:
			panic(errors.Errorf("unexpected installed package %s", installedPkg.Package))
		}
	}
	pkg.Version = strings.TrimSpace(dockerVersion)
	if !strings.Contains(containerdVersion, "1.4.3") {
		if pkg.Config == nil {
			pkg.Config = map[string]string{}
		}
		pkg.Config["containerd.io"] = containerdVersion
	} else {
		// Deprecated Code: Ensure existing containerd versions get locked
		// TODO: Remove in ORBOS v4
		lock, err := exec.CommandContext(c.ctx, "yum", "versionlock", "list").Output()
		if err != nil {
			return pkg, err
		}
		if !strings.Contains(string(lock), "containerd.io-1.4.3") {
			if pkg.Config == nil {
				pkg.Config = map[string]string{}
			}
			pkg.Config["containerd.io"] = containerdVersion
		}
	}

	daemonJson, _ := ioutil.ReadFile("/etc/docker/daemon.json")
	if pkg.Config == nil {
		pkg.Config = map[string]string{}
	}
	pkg.Config["daemon.json"] = string(daemonJson)
	return pkg, nil
}

func (c *criDep) Ensure(_ common.Package, install common.Package) error {

	if install.Config == nil {
		return errors.New("Docker config is nil")
	}

	if err := os.MkdirAll("/etc/docker", 600); err != nil {
		return err
	}

	if err := ioutil.WriteFile("/etc/docker/daemon.json", []byte(install.Config["daemon.json"]), 600); err != nil {
		return err
	}

	fields := strings.Fields(install.Version)
	if len(fields) != 2 {
		return errors.Errorf("Container runtime must have the form [runtime] [version], but got %s", install)
	}

	if fields[0] != "docker-ce" {
		return errors.New("Only docker-ce is supported yet")
	}

	version := strings.TrimLeft(fields[1], "v")

	switch c.os.OperatingSystem {
	case dep.Ubuntu:
		return c.ensureUbuntu(fields[0], version)
	case dep.CentOS:
		return c.ensureCentOS(fields[0], version)
	}
	return errors.Errorf("Operating %s system is not supported", c.os)
}
