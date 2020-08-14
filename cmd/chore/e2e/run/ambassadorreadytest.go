package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/ghodss/yaml"
)

func ambassadorReadyTest(orbctl newOrbctlCommandFunc, _ newKubectlCommandFunc) error {

	cmd, err := orbctl()
	if err != nil {
		return err
	}

	buf := &bytes.Buffer{}
	cmd.Args = append(cmd.Args, "file", "print", "caos-internal/orbiter/current.yml")
	cmd.Stdout = buf

	currentBytes, err := ioutil.ReadAll(buf)
	if err != nil {
		return err
	}

	current := struct {
		Providers struct {
			ProviderUnderTest struct {
				Current struct {
					Ingresses struct {
						Httpsingress struct {
							Location     string
							Frontendport uint16
						}
					}
				}
			} `yaml:"provider-under-test"`
		}
	}{}

	if err := yaml.Unmarshal(currentBytes, &current); err != nil {
		return err
	}

	ep := current.Providers.ProviderUnderTest.Current.Ingresses.Httpsingress
	resp, err := http.Get(fmt.Sprintf("https://%s:%d/ambassador/v0/check_ready", ep.Location, ep.Frontendport))
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return errors.New(resp.Status)
	}
	return nil
}