package swap

import (
	"bytes"
	"io"
	"os/exec"
	"strings"

	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
)

type Installer interface {
	isSwap()
	nodeagent.Installer
}

type swapDep struct {
	fstabFilePath string
}

func New(fstabFilePath string) Installer {
	return &swapDep{fstabFilePath}
}

func (swapDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (swapDep) isSwap() {}

func (swapDep) String() string { return "Swap" }

func (*swapDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*swapDep)
	return ok
}

func (s *swapDep) Current() (pkg common.Package, err error) {

	buf := new(bytes.Buffer)
	defer buf.Reset()

	swapon := exec.Command("swapon", "--summary")
	swapon.Stdout = buf
	if err := swapon.Run(); err != nil {
		return pkg, err
	}

	pkg.Version = "disabled"
	var lines uint8
	var line string
	for {
		if err != nil && err != io.EOF {
			return pkg, err
		}
		line, err = buf.ReadString('\n')
		if len(line) > 0 {
			lines++
		}
		if lines >= 2 {
			pkg.Version = "enabled"
			return
		}
		if err == io.EOF {
			return pkg, nil
		}
	}
}

func (s *swapDep) Ensure(remove common.Package, ensure common.Package, _ bool) error {
	buf := new(bytes.Buffer)
	defer buf.Reset()

	swapoff := exec.Command("swapoff", "--all")
	swapoff.Stderr = buf
	if err := swapoff.Run(); err != nil {
		return errors.Wrapf(err, "Disabling swap failed with standard error: %s", buf.String())
	}

	return dep.ManipulateFile(s.fstabFilePath, nil, nil, func(line string) *string {
		if !strings.Contains(line, "swap") {
			return &line
		}
		switch {
		case strings.HasPrefix(line, "#") && ensure.Version == "enabled" && remove.Version == "disabled":
			line = strings.Replace(line, "#", "", 1)
		case !strings.HasPrefix(line, "#") && ensure.Version == "disabled" && remove.Version == "enabled":
			line = "#" + line
		}
		return &line
	})
}
