package gce

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/providers/ssh"
	"github.com/caos/orbos/mntr"
)

type gceMachine struct {
	mntr.Monitor
	id      string
	zone    string
	context *context
}

func newGCEMachine(context *context, monitor mntr.Monitor, id string, zone string) machine {
	return &gceMachine{
		Monitor: monitor,
		id:      id,
		zone:    zone,
		context: context,
	}
}

func resetBuffer(buffer *bytes.Buffer) {
	if buffer != nil {
		buffer.Reset()
	}
}

func (c *gceMachine) Zone() string {
	return c.zone
}

func (c *gceMachine) Execute(stdin io.Reader, command string) ([]byte, error) {
	buf, err := c.execute(stdin, command)
	defer resetBuffer(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *gceMachine) execute(stdin io.Reader, command string) (outBuf *bytes.Buffer, err error) {
	outBuf = new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	defer resetBuffer(errBuf)

	gcloud, err := exec.LookPath("gcloud")
	if err != nil {
		return nil, err
	}

	if err := gcloudSession(c.context.desired.JSONKey.Value, gcloud, func(bin string) error {
		cmd := exec.Command(gcloud,
			"compute",
			"ssh",
			"--zone", c.zone,
			fmt.Sprintf("orbiter@%s", c.id),
			"--tunnel-through-iap",
			"--project", c.context.projectID,
			"--command", command,
		)
		cmd.Stdin = stdin
		cmd.Stdout = outBuf
		cmd.Stderr = errBuf
		if runErr := cmd.Run(); runErr != nil {
			return errors.New(errBuf.String())
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return outBuf, nil
}

func (c *gceMachine) Shell() error {
	errBuf := new(bytes.Buffer)
	defer resetBuffer(errBuf)

	gcloud, err := exec.LookPath("gcloud")
	if err != nil {
		return err
	}

	if err := gcloudSession(c.context.desired.JSONKey.Value, gcloud, func(bin string) error {
		cmd := exec.Command(gcloud,
			"compute",
			"ssh",
			"--zone", c.zone,
			fmt.Sprintf("orbiter@%s", c.id),
			"--tunnel-through-iap",
			"--project", c.context.projectID,
		)
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		return cmd.Run()
	}); err != nil {
		return err
	}
	return nil
}

func (c *gceMachine) WriteFile(path string, data io.Reader, permissions uint16) error {

	user, err := c.Execute(nil, "whoami")
	if err != nil {
		return err
	}

	mkdir, writeFile := ssh.WriteFileCommands(strings.TrimSpace(string(user)), path, permissions)
	_, err = c.Execute(nil, mkdir)
	if err != nil {
		return err
	}

	_, err = c.Execute(data, writeFile)
	return err
}

func (c *gceMachine) ReadFile(path string, data io.Writer) error {
	buf, err := c.execute(nil, fmt.Sprintf("sudo cat %s", path))
	defer resetBuffer(buf)
	if err != nil {
		return err
	}

	_, err = io.Copy(data, buf)
	return err
}
