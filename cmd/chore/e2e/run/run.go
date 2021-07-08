package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"reflect"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/afiskon/promtail-client/promtail"
)

var _ runFunc = run

func run(ctx context.Context, settings programSettings) error {

	newOrbctl, err := buildOrbctl(ctx, settings)
	if err != nil {
		return err
	}

	if settings.cleanup {
		defer func() {
			// context is probably cancelled as program is terminating so we create a new one here
			destroyCtx, destroyCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer destroyCancel()
			if cuErr := destroy(&testSpecs{}, settings, zeroConditions())(destroyCtx, 99, newOrbctl); cuErr != nil {

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

	return seq(ctx, testSpecs{
		DesireORBITERState: struct {
			InitialMasters int
			InitialWorkers int
		}{
			InitialMasters: 3,
			InitialWorkers: 3,
		},
	}, settings, newOrbctl, configureKubectl(kubeconfig.Name()), readKubeconfig,
		/*  1 */ desireORBITERState,
		/*  2 */ destroy,
		/*  3 */ desireORBITERState,
		/*  4 */ bootstrap,
		/*  5 */ desireBOOMState(true),
		/*  6 */ downscale,
		/*  7 */ reboot,
		/*  8 */ replace,
		/*  9 */ upgrade("v1.21.0"),
	)
}

var _ fmt.Stringer = (*programSettings)(nil)

type programSettings struct {
	ctx                          context.Context
	logger                       promtail.Client
	orbID, branch, orbconfig     string
	from                         uint8
	cleanup, debugOrbctlCommands bool
	cache                        struct {
		artifactsVersion string
	}
}

func (s programSettings) artifactsVersion() string {
	if s.cache.artifactsVersion != "" {
		return s.cache.artifactsVersion
	}
	branchParts := strings.Split(s.branch, "/")
	s.cache.artifactsVersion = branchParts[len(branchParts)-1:][0] + "-dev"
	return s.cache.artifactsVersion
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

type testSpecs struct {
	DesireORBITERState struct {
		InitialMasters int
		InitialWorkers int
	}
	DesireBOOMState struct {
		Stateless bool
	}
}

type testFunc func(*testSpecs, programSettings, *conditions) interactFunc

type interactFunc func(context.Context, uint8, newOrbctlCommandFunc) (err error)

func seq(
	ctx context.Context,
	defaultSpecs testSpecs,
	settings programSettings,
	newOrbctl newOrbctlCommandFunc,
	newKubectl newKubectlCommandFunc,
	downloadKubeconfigFunc downloadKubeconfig,
	fns ...testFunc,
) error {

	conditions := zeroConditions()

	e2eSpecBuf := new(bytes.Buffer)
	defer e2eSpecBuf.Reset()

	if err := runCommand(settings, nil, e2eSpecBuf, nil, newOrbctl(ctx), "--gitops", "file", "print", "e2e.yml"); err != nil {
		return err
	}

	if err := yaml.Unmarshal(e2eSpecBuf.Bytes(), &defaultSpecs); err != nil {
		return err
	}

	var at uint8
	for _, fn := range fns {
		at++

		// must be called before continue so we keep having an idempotent desired state
		interactFn := fn(&defaultSpecs, settings, conditions)

		if at < settings.from {
			settings.logger.Infof("\033[1;32m%s: Skipping step %d\033[0m\n", settings.orbID, at)
			continue
		}

		if err := runTest(ctx, settings, interactFn, newOrbctl, newKubectl, downloadKubeconfigFunc, at, conditions); err != nil {
			return err
		}
	}
	return nil
}

func runTest(
	ctx context.Context,
	settings programSettings,
	fn interactFunc,
	orbctl newOrbctlCommandFunc,
	kubectl newKubectlCommandFunc,
	downloadKubeconfigFunc downloadKubeconfig,
	step uint8,
	conditions *conditions,
) (err error) {
	fnName := fmt.Sprintf("%s (%d. in stack)", runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name(), step)

	defer func() {
		if err != nil {
			err = fmt.Errorf("%s failed: %w", fnName, err)
		} else {
			settings.logger.Infof("\033[1;32m%s: %s succeeded\033[0m\n", settings.orbID, fnName)
		}
	}()

	testContext, testCancel := context.WithCancel(ctx)
	defer testCancel()

	if err := fn(testContext, step, orbctl); err != nil {
		return err
	}

	var awaitFuncs []func() error

	appendAwaitFunc := func(condition *condition) {
		if condition != nil {
			awaitFuncs = append(awaitFuncs, func() error {
				return awaitCondition(testContext, settings, orbctl, kubectl, downloadKubeconfigFunc, step, condition)
			})
		}
	}

	appendAwaitFunc(conditions.testCase)
	appendAwaitFunc(conditions.orbiter)
	appendAwaitFunc(conditions.boom)

	for _, awaitFunc := range awaitFuncs {
		if err := awaitFunc(); err != nil {
			return err
		}
	}

	return nil
}
