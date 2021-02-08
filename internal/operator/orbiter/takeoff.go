package orbiter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"

	"github.com/caos/orbos/internal/api"
	"github.com/caos/orbos/internal/operator/common"
	orbconfig "github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
)

func ToEnsureResult(done bool, err error) *EnsureResult {
	return &EnsureResult{
		Err:  err,
		Done: done,
	}
}

type EnsureResult struct {
	Err  error
	Done bool
}

type ConfigureFunc func(orb orbconfig.Orb) error

func NoopConfigure(orb orbconfig.Orb) error {
	return nil
}

type QueryFunc func(ctx context.Context, nodeAgentsCurrent *common.CurrentNodeAgents, nodeAgentsDesired *common.DesiredNodeAgents, queried map[string]interface{}) (EnsureFunc, error)

type EnsureFunc func(pdf api.PushDesiredFunc) *EnsureResult

func NoopEnsure(_ api.PushDesiredFunc) *EnsureResult {
	return &EnsureResult{Done: true}
}

type retQuery struct {
	ensure EnsureFunc
	err    error
}

func QueryFuncGoroutine(query func() (EnsureFunc, error)) (EnsureFunc, error) {
	retChan := make(chan retQuery)
	go func() {
		ensure, err := query()
		retChan <- retQuery{ensure, err}
	}()
	ret := <-retChan
	return ret.ensure, ret.err
}

func EnsureFuncGoroutine(ensure func() *EnsureResult) *EnsureResult {
	retChan := make(chan *EnsureResult)
	go func() {
		retChan <- ensure()
	}()
	return <-retChan
}

type event struct {
	commit string
	files  []git.File
}

func Instrument(monitor mntr.Monitor, healthyChan chan bool) {
	healthy := true

	prometheus.MustRegister(prometheus.NewBuildInfoCollector())
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(writer http.ResponseWriter, request *http.Request) {
		msg := "OK"
		status := 200
		if !healthy {
			msg = "ORBITER is not healthy. See the logs."
			status = 404
		}
		writer.WriteHeader(status)
		writer.Write([]byte(msg))
	})

	go func() {
		timeout := 10 * time.Minute
		ticker := time.NewTimer(timeout)
		for {
			select {
			case newHealthiness := <-healthyChan:
				ticker.Reset(timeout)
				if newHealthiness == healthy {
					continue
				}
				healthy = newHealthiness
				if !newHealthiness {
					monitor.Error(errors.New("ORBITER is unhealthy now"))
					continue
				}
				monitor.Info("ORBITER is healthy now")
			case <-ticker.C:
				monitor.Error(errors.New("ORBITER is unhealthy now as it did not report healthiness for 10 minutes"))
				healthy = false
			}
		}
	}()

	if err := http.ListenAndServe(":9000", nil); err != nil {
		panic(err)
	}
}

func Adapt(gitClient *git.Client, monitor mntr.Monitor, finished chan struct{}, adapt AdaptFunc) (QueryFunc, DestroyFunc, ConfigureFunc, bool, *tree.Tree, *tree.Tree, map[string]*secret.Secret, error) {

	treeDesired, err := api.ReadOrbiterYml(gitClient)
	if err != nil {
		return nil, nil, nil, false, nil, nil, nil, err
	}
	treeCurrent := &tree.Tree{}

	adaptFunc := func() (QueryFunc, DestroyFunc, ConfigureFunc, bool, map[string]*secret.Secret, error) {
		return adapt(monitor, finished, treeDesired, treeCurrent)
	}
	query, destroy, configure, migrate, secrets, err := AdaptFuncGoroutine(adaptFunc)
	return query, destroy, configure, migrate, treeDesired, treeCurrent, secrets, err
}

func Takeoff(ctx context.Context, monitor mntr.Monitor, conf *Config, healthyChan chan bool) func() {

	return func() {

		itCtx, itCancel := context.WithCancel(ctx)
		defer itCancel()

		var err error
		defer func() {
			go func() {
				if err != nil {
					healthyChan <- false
					return
				}
				healthyChan <- true
			}()
		}()

		query, _, _, migrate, treeDesired, treeCurrent, _, err := Adapt(conf.GitClient, monitor, conf.FinishedChan, conf.Adapt)
		if err != nil {
			monitor.Error(err)
			return
		}

		desiredNodeAgents := common.NodeAgentsDesiredKind{
			Kind:    "nodeagent.caos.ch/NodeAgents",
			Version: "v0",
			Spec: common.NodeAgentsSpec{
				Commit: conf.OrbiterCommit,
			},
		}

		marshalCurrentFiles := func() []git.File {
			return []git.File{{
				Path:    "caos-internal/orbiter/current.yml",
				Content: common.MarshalYAML(treeCurrent),
			}, {
				Path:    "caos-internal/orbiter/node-agents-desired.yml",
				Content: common.MarshalYAML(desiredNodeAgents),
			}}
		}

		if migrate {
			if err = api.PushOrbiterYml(monitor, "Desired state migrated", conf.GitClient, treeDesired); err != nil {
				monitor.Error(err)
				return
			}
		}

		currentNodeAgents := common.NodeAgentsCurrentKind{}
		if err := yaml.Unmarshal(conf.GitClient.Read("caos-internal/orbiter/node-agents-current.yml"), &currentNodeAgents); err != nil {
			monitor.Error(err)
			return
		}

		handleAdapterError := func(err error) {
			monitor.Error(err)
			if commitErr := conf.GitClient.Commit(mntr.CommitRecord([]*mntr.Field{{Pos: 0, Key: "err", Value: err.Error()}})); commitErr != nil {
				monitor.Error(err)
				return
			}
			monitor.Error(conf.GitClient.Push())
		}

		queryFunc := func() (EnsureFunc, error) {
			return query(itCtx, &currentNodeAgents.Current, &desiredNodeAgents.Spec.NodeAgents, nil)
		}
		ensure, err := QueryFuncGoroutine(queryFunc)
		if err != nil {
			handleAdapterError(err)
			return
		}

		if err := conf.GitClient.Clone(); err != nil {
			monitor.Error(err)
			return
		}

		reconciledCurrentStateMsg := "Current state reconciled"
		currentReconciled, err := conf.GitClient.StageAndCommit(mntr.CommitRecord([]*mntr.Field{{Key: "evt", Value: reconciledCurrentStateMsg}}), marshalCurrentFiles()[0])
		if err != nil {
			monitor.Error(fmt.Errorf("Commiting event \"%s\" failed: %s", reconciledCurrentStateMsg, err.Error()))
			return
		}

		if currentReconciled {
			if err := conf.GitClient.Push(); err != nil {
				monitor.Error(fmt.Errorf("Pushing event \"%s\" failed: %s", reconciledCurrentStateMsg, err.Error()))
				return
			}
		}

		ensureFunc := func() *EnsureResult {
			return ensure(api.PushOrbiterDesiredFunc(conf.GitClient, treeDesired))
		}

		result := EnsureFuncGoroutine(ensureFunc)
		if result.Err != nil {
			handleAdapterError(result.Err)
			return
		}

		if result.Done {
			monitor.Info("Desired state is ensured")
		} else {
			monitor.Info("Desired state is not yet ensured")
		}
		if err := conf.GitClient.Clone(); err != nil {
			monitor.Error(fmt.Errorf("Commiting event \"%s\" failed: %s", reconciledCurrentStateMsg, err.Error()))
			return
		}

		changed, err := conf.GitClient.StageAndCommit("Current state changed", marshalCurrentFiles()...)
		if err != nil {
			monitor.Error(fmt.Errorf("commiting current state failed: %w", err))
			return
		}

		if changed {
			pushErr := conf.GitClient.Push()
			if err == nil {
				err = pushErr
			}
			monitor.Error(err)
		}
	}
}
