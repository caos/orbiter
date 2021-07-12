package dep

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

func (p *PackageManager) rembasedAdd(repo *Repository) error {

	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()
	outBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	cmd := exec.CommandContext(p.ctx, "yum-config-manager", "--add-repo", repo.Repository)
	cmd.Stderr = errBuf
	cmd.Stdout = outBuf
	err := cmd.Run()

	out := outBuf.String()
	if p.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		fmt.Println(out)
	}

	if err != nil && !strings.Contains(out, fmt.Sprintf("Cannot add repo from %s as is a duplicate of an existing repo", repo.Repository)) {
		return errors.Wrapf(err, "adding yum repository %s failed with stderr %s", repo.Repository, out)
	}

	return nil
}

func (p *PackageManager) debbasedAdd(repo *Repository) error {

	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()

	resp, err := http.Get(repo.KeyURL)
	if err != nil {
		return errors.Wrapf(err, "getting key from url %s failed", repo.KeyURL)
	}
	defer resp.Body.Close()
	cmd := exec.CommandContext("apt-key", "add", "-")
	cmd.Stdin = resp.Body
	cmd.Stderr = errBuf

	if p.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}

	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "adding key failed with stderr %s", errBuf.String())
	}
	errBuf.Reset()
	p.monitor.WithFields(map[string]interface{}{
		"url": repo.KeyURL,
	}).Debug("Added repository key from url")

	if repo.KeyFingerprint != "" {
		buf := new(bytes.Buffer)
		defer buf.Reset()

		cmd := exec.CommandContext(p.ctx, "apt-key", "fingerprint", repo.KeyFingerprint)
		cmd.Stdout = buf
		cmd.Stderr = errBuf
		if err := cmd.Run(); err != nil {
			return errors.Wrapf(err, "verifying fingerprint %s failed with stderr %s", repo.KeyFingerprint, errBuf.String())
		}

		if p.monitor.IsVerbose() {
			fmt.Println(strings.Join(cmd.Args, " "))
		}

		errBuf.Reset()
		p.monitor.WithFields(map[string]interface{}{
			"url":         repo.KeyURL,
			"fingerprint": repo.KeyFingerprint,
		}).Debug("Checked fingerprint")
		found := false
		for {
			line, err := buf.ReadString('\n')
			if p.monitor.IsVerbose() {
				fmt.Println(line)
			}

			if strings.HasPrefix(line, "uid") {
				p.monitor.WithFields(map[string]interface{}{
					"uid": strings.TrimSpace(strings.TrimPrefix(line, "uid")),
				}).Debug("Added and verified repository key")
				found = true
				break
			}
			if line == "\n" || err != nil {
				break
			}
		}
		if !found {
			return errors.Errorf("No key with fingerprint %s found", repo.KeyFingerprint)
		}
	}

	cmd = exec.CommandContext(p.ctx, "add-apt-repository", "-y", repo.Repository)
	cmd.Stderr = errBuf

	if p.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}

	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "adding repository %s failed with stderr %s", repo.Repository, errBuf.String())
	}
	errBuf.Reset()
	p.monitor.WithFields(map[string]interface{}{
		"repository": repo.Repository,
	}).Debug("Added repository")

	cmd = exec.CommandContext(p.ctx, "apt-get", strings.Fields("--assume-yes --allow-downgrades update")...)
	cmd.Stderr = errBuf
	if p.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "updating indices failed with stderr %s", errBuf.String())
	}
	errBuf.Reset()
	p.monitor.Debug("Updated index")
	return nil
}
