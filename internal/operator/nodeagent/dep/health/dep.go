package health

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caos/orbos/internal/operator/nodeagent/dep"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/middleware"
	"github.com/caos/orbos/mntr"
)

const dir string = "/lib/systemd/system"

type Installer interface {
	isHealth()
	nodeagent.Installer
}
type healthDep struct {
	monitor mntr.Monitor
	systemd *dep.SystemD
}

func New(monitor mntr.Monitor, systemd *dep.SystemD) Installer {
	return &healthDep{monitor: monitor, systemd: systemd}
}

func (healthDep) isHealth() {}

func (healthDep) Is(other nodeagent.Installer) bool {
	_, ok := middleware.Unwrap(other).(Installer)
	return ok
}

func (healthDep) String() string { return "health" }

func (*healthDep) Equals(other nodeagent.Installer) bool {
	_, ok := other.(*healthDep)
	return ok
}

var r = regexp.MustCompile(`ExecStart=/usr/local/bin/health --listen ([^\s]+) (.*)`)

func (s *healthDep) Current() (pkg common.Package, err error) {

	files, _ := ioutil.ReadDir(dir)
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "orbos.health.") {
			continue
		}
		content, err := ioutil.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			return pkg, err
		}

		if pkg.Config == nil {
			pkg.Config = make(map[string]string)
		}

		if s.systemd.Active(file.Name()) {
			http, checks := extractArguments(content)
			pkg.Config[http] = unquote(checks)
		}
	}
	return pkg, nil
}

func extractArguments(content []byte) (string, string) {
	match := r.FindStringSubmatch(string(content))
	return match[1], match[2]
}

func (s *healthDep) Ensure(_ common.Package, ensure common.Package, _ bool) error {

	files, _ := ioutil.ReadDir(dir)
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "orbos.health.") {
			if err := s.systemd.Disable(file.Name()); err != nil {
				return err
			}
			if err := os.Remove(filepath.Join(dir, file.Name())); err != nil {
				return err
			}
		}
	}

	var i int
	for location, args := range ensure.Config {
		i++
		svc := fmt.Sprintf("orbos.health.%d.service", i)
		if err := ioutil.WriteFile(filepath.Join(dir, svc), []byte(fmt.Sprintf(`
[Unit]
Description=Healthchecks Proxy
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/health --listen %s %s
Restart=always
MemoryLimit=20M
MemoryAccounting=yes
RestartSec=10
CPUAccounting=yes

[Install]
WantedBy=multi-user.target
`, location, quote(args))), 0600); err != nil {
			return err
		}

		if err := s.systemd.Enable(svc); err != nil {
			return err
		}
		if err := s.systemd.Start(svc); err != nil {
			return err
		}
	}

	return nil
}

func unquote(args string) string {
	s := strings.Split(args, " ")
	for idx, a := range s {
		s[idx] = strings.Trim(a, `"`)
	}
	return strings.Join(s, " ")
}

func quote(args string) string {
	s := strings.Split(args, " ")
	for idx, a := range s {
		s[idx] = fmt.Sprintf(`"%s"`, a)
	}
	return strings.Join(s, " ")
}
