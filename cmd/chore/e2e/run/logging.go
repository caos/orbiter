package main

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

func logWriter(log func(format string, args ...interface{})) (io.Writer, func()) {
	buf := new(bytes.Buffer)
	return buf, func() {
		defer buf.Reset()
		scanner := bufio.NewScanner(buf)
		for scanner.Scan() {
			log(scanner.Text())
		}
	}
}

func logStdout(settings programSettings, line string) {
	log := settings.logger.Infof
	if strings.Contains(line, ` err=`) {
		log = settings.logger.Warnf
	}
	log(line)
}
