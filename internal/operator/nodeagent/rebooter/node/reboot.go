package node

import (
	"os"
	"os/exec"

	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/rebooter"
)

func New() nodeagent.Rebooter {
	return rebooter.Func(func() error {
		if err := exec.CommandContext("reboot").Run(); err != nil {
			return err
		}
		os.Exit(0)
		return nil
	})
}
