package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

func runCommand(settings programSettings, logPrefix *string, write io.Writer, scan func(line string), cmd *exec.Cmd, args ...string) error {

	cmd.Args = append(cmd.Args, args...)

	errWriter, errWrite := logWriter(settings.logger.Warnf)
	defer errWrite()
	cmd.Stderr = errWriter

	stdoutReader, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}

	var scanReader io.Reader = stdoutReader

	if write != nil {
		scanReader = io.TeeReader(stdoutReader, write)
	}

	settings.logger.Debugf(fmt.Sprintf(`'%s'`, strings.Join(cmd.Args, `' '`)))

	if err := cmd.Start(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		panic(err)
	}
	scanner := bufio.NewScanner(scanReader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if scan != nil {
			scan(line)
		}

		if logPrefix != nil {
			logStdout(settings, *logPrefix+line)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return cmd.Wait()
}
