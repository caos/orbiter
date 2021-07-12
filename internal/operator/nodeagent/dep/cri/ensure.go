package cri

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/caos/orbos/internal/operator/nodeagent/dep"
)

func (c *criDep) ensureCentOS(runtime string, version string) error {
	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()
	cmd := exec.CommandContext("yum", "--assumeyes", "remove", "docker",
		"docker-client",
		"docker-client-latest",
		"docker-common",
		"docker-latest",
		"docker-latest-logrotate",
		"docker-logrotate",
		"docker-engine")
	cmd.Stderr = errBuf
	if c.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("removing older docker versions failed with stderr %s: %w", errBuf.String(), err)
	}

	for _, pkg := range []string{"device-mapper-persistent-data", "lvm2"} {
		if err := c.manager.Install(&dep.Software{Package: pkg}); err != nil {
			c.monitor.Error(fmt.Errorf("installing docker dependency failed: %w", err))
		}
	}

	return c.run(runtime, version, "https://download.docker.com/linux/centos/docker-ce.repo", "", "")
}

func (c *criDep) ensureUbuntu(runtime string, version string) error {

	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()
	buf := new(bytes.Buffer)
	defer buf.Reset()

	var versionLine string
	cmd := exec.CommandContext("apt-cache", "madison", runtime)
	cmd.Stderr = errBuf
	cmd.Stdout = buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running apt-cache madison %s failed with stderr %s: %w", runtime, errBuf.String(), err)
	}
	errBuf.Reset()

	if c.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
	}

	var err error
	for err == nil {
		versionLine, err = buf.ReadString('\n')
		if c.monitor.IsVerbose() {
			fmt.Println(versionLine)
		}
		if strings.Contains(versionLine, version) {
			break
		}
	}
	buf.Reset()

	if err != nil && versionLine == "" {
		return fmt.Errorf("finding line containing desired container runtime version \"%s\" failed: %w", version, err)
	}

	return c.run(
		runtime,
		strings.TrimSpace(strings.Split(versionLine, "|")[1]),
		fmt.Sprintf("deb [arch=amd64] https://download.docker.com/linux/ubuntu %s stable", c.os.Version),
		"https://download.docker.com/linux/ubuntu/gpg",
		"0EBFCD88",
	)
}

func (c *criDep) run(runtime, version, repoURL, keyURL, keyFingerprint string) error {

	try := func() error {
		// Obviously, docker doesn't care about the exact containerd version, so neighter should ORBITER
		// https://docs.docker.com/engine/install/centos/
		// https://docs.docker.com/engine/install/ubuntu/
		if err := c.manager.Install(&dep.Software{
			Package: "containerd.io",
			Version: containerdVersion,
		}); err != nil {
			return err
		}

		err := c.manager.Install(&dep.Software{
			Package: runtime,
			Version: version,
		})
		return err
	}

	if err := try(); err != nil {
		swmonitor := c.monitor.WithField("software", "docker")
		swmonitor.Error(fmt.Errorf("installing software from existing repo failed, trying again after adding repo: %w", err))

		if err := c.manager.Add(&dep.Repository{
			Repository:     repoURL,
			KeyURL:         keyURL,
			KeyFingerprint: keyFingerprint,
		}); err != nil {
			return err
		}
		swmonitor.WithField("url", repoURL).Info("repo added")

		if err := try(); err != nil {
			swmonitor.Error(fmt.Errorf("installing software from %s failed: %w", repoURL, err))
			return err
		}
	}

	if err := c.systemd.Enable("docker"); err != nil {
		return err
	}
	return c.systemd.Start("docker")
}
