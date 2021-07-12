package dep

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (p *PackageManager) debSpecificUpdatePackages() error {
	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()
	cmd := exec.CommandContext(p.ctx, "apt-get", "--assume-yes", "update")
	cmd.Stderr = errBuf
	if p.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("updating deb packages failed with stderr %s: %w", errBuf.String(), err)
	}
	return nil
}
func (p *PackageManager) debSpecificInit() error {
	return p.debbasedInstall(
		&Software{Package: "apt-transport-https"},
		&Software{Package: "gnupg2"},
		&Software{Package: "software-properties-common"})
}

func (p *PackageManager) remSpecificInit() error {

	if err := p.rembasedInstall(
		&Software{Package: "yum-utils"},
		&Software{Package: "yum-plugin-versionlock"},
		&Software{Package: "firewalld"},
	); err != nil {
		return err
	}

	return p.systemd.Enable("firewalld")
}

func (p *PackageManager) remSpecificUpdatePackages() error {

	conflictingCronFile := "/etc/cron.daily/yumupdate.sh"
	removeConflictingCronFile := true
	_, err := os.Stat(conflictingCronFile)
	if err != nil {
		if os.IsNotExist(err) {
			removeConflictingCronFile = false
			err = nil
		}
	}
	if err != nil {
		return err
	}
	if removeConflictingCronFile {
		if err := os.Remove(conflictingCronFile); err != nil {
			return err
		}
	}

	cmd := exec.CommandContext(p.ctx, "/usr/bin/yum", "--assumeyes", "--errorlevel", "0", "--debuglevel", "3", "update")
	errBuf := new(bytes.Buffer)
	defer errBuf.Reset()
	cmd.Stderr = errBuf
	if p.monitor.IsVerbose() {
		fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("updating yum packages failed with stderr %s: %w", errBuf.String(), err)
	}
	return nil
}
