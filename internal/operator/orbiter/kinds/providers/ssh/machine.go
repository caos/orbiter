package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/caos/orbos/internal/ssh"

	"github.com/caos/orbos/mntr"

	sshlib "golang.org/x/crypto/ssh"
)

type Machine struct {
	ctx        context.Context
	monitor    mntr.Monitor
	remoteUser string
	ip         string
	sshCfg     *sshlib.ClientConfig
}

func NewMachine(ctx context.Context, monitor mntr.Monitor, remoteUser, ip string) *Machine {
	return &Machine{
		ctx:        ctx,
		remoteUser: remoteUser,
		monitor: monitor.WithFields(map[string]interface{}{
			"host": ip,
			"user": remoteUser,
		}),
		ip: ip,
	}
}

func (c *Machine) Execute(stdin io.Reader, cmd string) (stdout []byte, err error) {

	monitor := c.monitor.WithFields(map[string]interface{}{
		"command": cmd,
	})
	defer func() {
		if err != nil {
			err = fmt.Errorf("executing %s failed: %w", cmd, err)
		} else {
			monitor.WithField("stdout", string(stdout)).Debug("Done executing command with ssh")
		}
	}()

	monitor.Debug("Trying to execute with ssh")

	var output []byte
	sess, close, err := c.open()
	defer close()
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	sess.Stdin = stdin
	sess.Stderr = buf

	output, err = sess.Output(cmd)
	if err != nil {
		return output, fmt.Errorf("stderr: %s", buf.String())
	}
	return output, nil
}

func (c *Machine) Shell() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("executing shell failed: %w", err)
		} else {
			c.monitor.Debug("Done executing shell with ssh")
		}
	}()

	sess, close, err := c.open()
	defer close()
	if err != nil {
		return err
	}
	sess.Stdin = os.Stdin
	sess.Stderr = os.Stderr
	sess.Stdout = os.Stdout
	modes := sshlib.TerminalModes{
		sshlib.ECHO:          0,     // disable echoing
		sshlib.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		sshlib.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	if err := sess.RequestPty("xterm", 40, 80, modes); err != nil {
		return fmt.Errorf("request for pseudo terminal failed: %w", err)
	}

	if err := sess.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}
	return sess.Wait()
}

func WriteFileCommands(user, path string, permissions uint16) (string, string) {
	return fmt.Sprintf("sudo mkdir -p %s && sudo chown -R %s %s", filepath.Dir(path), user, filepath.Dir(path)),
		fmt.Sprintf("sudo sh -c 'cat > %s && chmod %d %s && chown %s %s'", path, permissions, path, user, path)
}

func (c *Machine) WriteFile(path string, data io.Reader, permissions uint16) (err error) {

	monitor := c.monitor.WithFields(map[string]interface{}{
		"path":        path,
		"permissions": permissions,
	})
	defer func() {
		if err != nil {
			err = fmt.Errorf("writing file %s failed: %w", path, err)
		} else {
			monitor.Debug("Done writing file with ssh")
		}
	}()

	monitor.Debug("Trying to write file with ssh")

	ensurePath, writeFile := WriteFileCommands(c.remoteUser, path, permissions)

	if _, err := c.Execute(nil, ensurePath); err != nil {
		return err
	}

	_, err = c.Execute(data, writeFile)
	return err
}

func (c *Machine) ReadFile(path string, data io.Writer) (err error) {

	monitor := c.monitor.WithFields(map[string]interface{}{
		"path": path,
	})
	defer func() {
		if err != nil {
			err = fmt.Errorf("reading file %s failed: %w", path, err)
		} else {
			monitor.Debug("Done reading file with ssh")
		}
	}()

	monitor.Debug("Trying to read file with ssh")

	cmd := fmt.Sprintf("sudo cat %s", path)
	sess, close, err := c.open()
	defer close()
	if err != nil {
		return err
	}
	stderr := new(bytes.Buffer)
	sess.Stdout = data
	sess.Stderr = stderr

	if err := sess.Run(cmd); err != nil {
		return fmt.Errorf("executing %s failed with stderr %s: %w", cmd, stderr.String(), err)
	}
	return nil
}

func (c *Machine) open() (sess *sshlib.Session, close func(), err error) {

	c.monitor.Debug("Trying to open an ssh connection")
	close = func() {}

	if c.sshCfg == nil {
		return nil, close, errors.New("no ssh key passed via infra.Machine.UseKey")
	}

	address := fmt.Sprintf("%s:%d", c.ip, 22)
	d := net.Dialer{}
	conn, err := d.DialContext(c.ctx, "tcp", address)
	if err != nil {
		return nil, close, fmt.Errorf("dialling tcp at %s failed: %w", address, err)
	}

	sshConn, chans, reqs, err := sshlib.NewClientConn(conn, address, c.sshCfg)
	if err != nil {
		mustClose(conn)
		return nil, close, fmt.Errorf("creating SSH connection at %s with user %s failed: %w", address, c.remoteUser, err)
	}

	sess, err = sshlib.NewClient(sshConn, chans, reqs).NewSession()
	if err != nil {
		mustClose(sshConn)
		mustClose(conn)
		return nil, close, fmt.Errorf("creating SSH session failed: %w", err)
	}
	return sess, func() {
		mustClose(sess)
		mustClose(sshConn)
	}, nil
}

func mustClose(closer io.Closer) {
	if err := closer.Close(); err != nil && err.Error() != "EOF" {
		panic(err)
	}
}

func (c *Machine) UseKey(keys ...[]byte) error {

	publicKeys, err := ssh.AuthMethodFromKeys(keys...)
	if err != nil {
		return err
	}

	c.sshCfg = &sshlib.ClientConfig{
		User:            c.remoteUser,
		Auth:            []sshlib.AuthMethod{publicKeys},
		HostKeyCallback: sshlib.InsecureIgnoreHostKey(),
	}
	return nil
}
