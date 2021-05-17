package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os/exec"
	"reflect"
	"runtime"
	"syscall"
	"time"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/kubernetes"

	"github.com/afiskon/promtail-client/promtail"
)

var _ runFunc = run

func run(settings programSettings) error {

	newOrbctl, err := buildOrbctl(settings)
	if err != nil {
		return err
	}

	if settings.cleanup {
		defer func() {
			// context is probably cancelled as program is terminating so we create a new one here
			destroySettings := settings
			destroySettings.ctx = context.Background()
			if _, cuErr := destroyTestFunc(destroySettings, nil)(99, newOrbctl); cuErr != nil {

				original := ""
				if err != nil {
					original = fmt.Sprintf(": %s", err.Error())
				}

				err = fmt.Errorf("cleaning up after end-to-end test failed: %w%s", cuErr, original)
			}
		}()
	}

	kubeconfig, err := ioutil.TempFile("", "kubeconfig-*")
	if err != nil {
		return err
	}
	if err := kubeconfig.Close(); err != nil {
		return err
	}

	readKubeconfig, deleteKubeconfig := downloadKubeconfigFunc(settings, kubeconfig.Name())
	defer deleteKubeconfig()

	return seq(settings, newOrbctl, configureKubectl(kubeconfig.Name()), readKubeconfig,
		/*  1 */ writeInitialDesiredStateTest,
		/*  2 */ destroyTestFunc,
		/*  3 */ bootstrapTestFunc,
		/*  4 */ downscaleTestFunc,
		/*  5 */ upgradeTestFunc,
	)
}

var _ fmt.Stringer = (*programSettings)(nil)

type programSettings struct {
	ctx                          context.Context
	logger                       promtail.Client
	orbID, branch, orbconfig     string
	from                         uint8
	cleanup, debugOrbctlCommands bool
}

func (p *programSettings) String() string {
	return fmt.Sprintf(`orbconfig=%s
orb=%s
branch=%s
from=%d
cleanup=%t`,
		p.orbconfig,
		p.orbID,
		p.branch,
		p.from,
		p.cleanup,
	)
}

type testFunc func(programSettings, *kubernetes.Spec) interactFunc

type interactFunc func(uint8, newOrbctlCommandFunc) (time.Duration, error)

func seq(settings programSettings, orbctl newOrbctlCommandFunc, kubectl newKubectlCommandFunc, downloadKubeconfigFunc downloadKubeconfig, fns ...testFunc) error {

	expect := &kubernetes.Spec{}

	var at uint8
	for _, fn := range fns {
		at++

		// must be called before continue, then we keep having an idempotent desired state
		interactFn := fn(settings, expect)

		if at < settings.from {
			settings.logger.Infof("\033[1;32m%s: Skipping step %d\033[0m\n", settings.orbID, at)
			continue
		}

		if err := runTest(settings, interactFn, orbctl, kubectl, downloadKubeconfigFunc, at, expect); err != nil {
			return err
		}
	}
	return nil
}

func runTest(settings programSettings, fn interactFunc, orbctl newOrbctlCommandFunc, kubectl newKubectlCommandFunc, downloadKubeconfigFunc downloadKubeconfig, step uint8, expect *kubernetes.Spec) (err error) {
	fnName := fmt.Sprintf("%s (%d. in stack)", runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name(), step)

	defer func() {
		if err != nil {
			err = fmt.Errorf("%s failed: %w", fnName, err)
		} else {
			settings.logger.Infof("\033[1;32m%s: %s succeeded\033[0m\n", settings.orbID, fnName)
		}
	}()

	timeout, err := fn(step, orbctl)
	if err != nil || timeout == 0 {
		return err
	}

	return awaitORBITER(settings, timeout, orbctl, kubectl, downloadKubeconfigFunc, step, expect)
}

func retry(count uint8, fn func() error) error {
	err := fn()
	exitErr := &exec.ExitError{}
	if err != nil &&
		// Don't retry if context timed out or was cancelled
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) &&
		// Don't retry if commands context timed out or was cancelled
		(!errors.As(err, &exitErr) ||
			!exitErr.Sys().(syscall.WaitStatus).Signaled()) &&
		count > 0 {
		return retry(count-1, fn)
	}
	return err
}
