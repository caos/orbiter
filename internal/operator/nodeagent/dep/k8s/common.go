package k8s

import (
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
)

type Common struct {
	manager    *dep.PackageManager
	os         dep.OperatingSystem
	normalizer *regexp.Regexp
	pkg        string
}

func New(os dep.OperatingSystem, manager *dep.PackageManager, pkg string) *Common {
	return &Common{manager, os, regexp.MustCompile(`\d+\.\d+\.\d+`), pkg}
}

func (c *Common) Current() (pkg common.Package, err error) {
	installed, err := c.manager.CurrentVersions(c.pkg)
	if err != nil {
		return pkg, errors.Wrapf(err, "getting current %s version failed", c.pkg)
	}
	if len(installed) == 0 {
		return pkg, nil
	}
	pkg.Version = "v" + c.normalizer.FindString(installed[0].Version)
	return pkg, nil
}

func (c *Common) Ensure(remove common.Package, install common.Package) error {

	switch c.os {
	case dep.Ubuntu:
		if err := c.manager.Add(&dep.Repository{
			KeyURL:         "https://packages.cloud.google.com/apt/doc/apt-key.gpg",
			KeyFingerprint: "",
			Repository:     "deb https://apt.kubernetes.io/ kubernetes-xenial main",
		}); err != nil {
			return errors.Wrap(err, "adding before installing kubeadm failed")
		}
	case dep.CentOS:
		ioutil.WriteFile("/etc/yum.repos.d/kubernetes.repo", []byte(`[kubernetes]
name=Kubernetes
baseurl=https://packages.cloud.google.com/yum/repos/kubernetes-el7-x86_64
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=https://packages.cloud.google.com/yum/doc/yum-key.gpg https://packages.cloud.google.com/yum/doc/rpm-package-key.gpg`), 0644)

		//		errBuf := new(bytes.Buffer)
		//		defer errBuf.Reset()
		//
		//		cmd := exec.Command("yum-config-manager", "--save", "--setopt=kubernetes.skip_if_unavailable=true")
		//		cmd.Stderr = &errBuf
		//		if c.monitor.IsVerbose() {
		//			fmt.Println(strings.Join(cmd.Args, " "))
		//			cmd.Stdout = os.Stdout
		//		}
		//		if err := cmd.Run(); err != nil {
		//			return errors.Wrapf(err, "unholding installed package failed with stderr %s", errBuf.String())
		//		}

	}

	pkgVersion := strings.TrimLeft(install.Version, "v") + "-0"
	if c.os == dep.Ubuntu {
		pkgVersion += "0"
	}

	if err := c.manager.Install(&dep.Software{Package: c.pkg, Version: pkgVersion}); err != nil {
		return errors.Wrapf(err, "installing %s failed", c.pkg)
	}
	return nil
}
