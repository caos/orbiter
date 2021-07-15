package mntr

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

// need to be global variables so that clients can be removed too
var (
	sentryClient   *sentry.Client
	env, comp, rel string
	dsns           map[string]string
	doIngest       bool
	semrel         = regexp.MustCompile("^v?[0-9]+.[0-9]+.[0-9]$")
)

func Ingest(monitor Monitor, codebase, version, environment, component string, moreComponents ...string) error {
	if rel != "" {
		panic("Ingest was already called")
	}
	if codebase == "" || version == "" || environment == "" || component == "" {
		panic("codebase, version, environment and component must not be empty")
	}

	if !semrel.Match([]byte(version)) {
		version = "dev"
	}
	rel = fmt.Sprintf("%s-%s", codebase, version)
	comp = strings.ToLower(component)
	env = strings.ToLower(environment)
	doIngest = true

	components := append([]string{component}, moreComponents...)

	go func() {
		for range time.NewTicker(15 * time.Minute).C {
			monitor.Error(fetchDSNs(components))
		}
	}()

	return fetchDSNs(components)
}

func fetchDSNs(keys []string) error {

	for idx := range keys {
		if err := fetchDSN(keys[idx]); err != nil {
			return err
		}
	}

	configure()
	return nil
}

func fetchDSN(key string) error {
	if dsns == nil {
		dsns = make(map[string]string)
	}

	resp, err := http.Get("https://raw.githubusercontent.com/caos/sentry-dsns/main/" + key)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	dsns[key] = strings.TrimSuffix(string(body), "\n")
	return nil
}

func (m Monitor) SwitchEnvironment(environment string) {
	if environment == "" {
		panic("environment must not be empty")
	}

	m.WithFields(map[string]interface{}{"from": env, "to": environment}).CaptureMessage("Environment changed")

	env = environment

	configure()

}

func Environment() (string, map[string]string, bool) {
	return env, dsns, doIngest
}

func configure() {

	if sentryClient != nil {
		sentryClient.Flush(time.Second * 2)
	}

	if env == "" || rel == "" {
		panic(errors.New("call mntr.Ingest first"))
	}

	var err error
	sentryClient, err = sentry.NewClient(sentry.ClientOptions{
		Dsn:         dsns[comp],
		Environment: env,
		Release:     rel,
		Debug:       false,
	})
	if err != nil {
		panic(err)
	}
}

func (m Monitor) captureWithFields(capture func(client *sentry.Client, scope sentry.EventModifier)) {
	if !doIngest {
		return
	}

	fields := normalize(m.Fields)
	for k, v := range fields {
		if v == "" {
			fields[k] = "none"
		}
	}
	fields["component"] = comp

	scope := sentry.NewScope()
	scope.SetTags(fields)
	capture(sentryClient, scope)
}
