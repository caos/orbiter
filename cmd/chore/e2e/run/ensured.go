package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/caos/orbos/internal/helpers"

	"github.com/caos/orbos/internal/operator/common"

	"gopkg.in/yaml.v3"
)

func awaitCondition(
	ctx context.Context,
	settings programSettings,
	orbctl newOrbctlCommandFunc,
	kubectl newKubectlCommandFunc,
	downloadKubeconfigFunc downloadKubeconfig,
	step uint8,
	condition *condition,
) error {

	awaitCtx, awaitCancel := context.WithTimeout(ctx, condition.watcher.timeout)
	defer awaitCancel()

	if err := downloadKubeconfigFunc(awaitCtx, orbctl); err != nil {
		return err
	}

	triggerCheck := make(chan struct{})
	trigger := func() { triggerCheck <- struct{}{} }
	// show initial state and tracking progress begins
	go trigger()

	go watchLogs(awaitCtx, settings, kubectl, condition.watcher, triggerCheck)

	started := time.Now()

	// Check each minute if the desired state is ensured
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	var err error

	for {
		select {
		case <-ticker.C:
			go trigger()
		case <-awaitCtx.Done():
			return helpers.Concat(awaitCtx.Err(), err)
		case <-triggerCheck:

			if err = isEnsured(awaitCtx, settings, orbctl, kubectl, condition); err != nil {
				printProgress(condition.watcher.logPrefix, settings, strconv.Itoa(int(step)), started, condition.watcher.timeout)
				settings.logger.Warnf("desired state is not yet ensured: %s", err.Error())
				continue
			}
			return nil
		}
	}
}

func watchLogs(ctx context.Context, settings programSettings, kubectl newKubectlCommandFunc, watcher watcher, lineFound chan<- struct{}) {

	select {
	case <-ctx.Done():
		return
	default:
		// goon
	}

	err := runCommand(settings, watcher.logPrefix.strPtr(), nil, func(line string) {
		// Check if the desired state is ensured when orbiter prints so
		if strings.Contains(line, watcher.checkWhenLogContains) {
			go func() { lineFound <- struct{}{} }()
		}
	}, kubectl(ctx), "logs", "--namespace", "caos-system", "--selector", watcher.selector, "--since", "10s", "--follow")

	if err != nil {
		settings.logger.Warnf("watching logs failed: %s. trying again", err.Error())
	}

	time.Sleep(1 * time.Second)

	watchLogs(ctx, settings, kubectl, watcher, lineFound)
}

func isEnsured(ctx context.Context, settings programSettings, newOrbctl newOrbctlCommandFunc, newKubectl newKubectlCommandFunc, condition *condition) error {

	if condition.checks == nil {
		return nil
	}

	var (
		orbiter    = currentOrbiter{}
		nodeagents = common.NodeAgentsCurrentKind{}
	)

	if err := helpers.Fanout([]func() error{
		func() error {
			return checkPodsAreReady(ctx, settings, newKubectl, "caos-system", condition.watcher.selector, 1)
		},
		func() error {
			return readYaml(ctx, settings, newOrbctl, &orbiter, "--gitops", "file", "print", "caos-internal/orbiter/current.yml")
		},
		func() error {
			return readYaml(ctx, settings, newOrbctl, &nodeagents, "--gitops", "file", "print", "caos-internal/orbiter/node-agents-current.yml")
		},
	})(); err != nil {
		return err
	}

	return condition.checks(ctx, newKubectl, orbiter, nodeagents)
}

func readYaml(ctx context.Context, settings programSettings, binFunc func(context.Context) *exec.Cmd, into interface{}, args ...string) error {

	orbctlCtx, orbctlCancel := context.WithTimeout(ctx, 10*time.Second)
	defer orbctlCancel()

	buf := new(bytes.Buffer)
	defer buf.Reset()

	if err := runCommand(settings, nil, buf, nil, binFunc(orbctlCtx), args...); err != nil {
		return fmt.Errorf("reading orbiters current state failed: %w", err)
	}

	currentBytes, err := ioutil.ReadAll(buf)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(currentBytes, into)
}

func checkPodsAreReady(ctx context.Context, settings programSettings, kubectl newKubectlCommandFunc, namespace, selector string, expectedPodsCount uint8) (err error) {

	defer func() {
		if err != nil {
			err = fmt.Errorf(`check for ready pods in namespace %s with selector "%s"" failed: %w`, namespace, selector, err)
		}
	}()

	pods := struct {
		Items []struct {
			Metadata struct {
				Name string
			}
			Status struct {
				Conditions []struct {
					Type   string
					Status string
				}
			}
		}
	}{}

	args := []string{
		"get", "pods",
		"--namespace", namespace,
		"--output", "yaml",
	}

	if selector != "" {
		args = append(args, "--selector", selector)
	}

	if err := readYaml(ctx, settings, kubectl, &pods, args...); err != nil {
		return err
	}

	podsCount := uint8(len(pods.Items))
	if podsCount != expectedPodsCount {
		return fmt.Errorf("%d pods are existing instead of %d", podsCount, expectedPodsCount)
	}

	for i := range pods.Items {
		pod := pods.Items[i]
		isReady := false
		for j := range pod.Status.Conditions {
			condition := pod.Status.Conditions[j]
			if condition.Type != "Ready" {
				continue
			}
			if condition.Status != "True" {
				return fmt.Errorf("pod %s has Ready condition %s", pod.Metadata.Name, condition.Status)
			}
			isReady = true
			break
		}
		if !isReady {
			return fmt.Errorf("pod %s has no Ready condition", pod.Metadata.Name)
		}
	}

	return nil
}
