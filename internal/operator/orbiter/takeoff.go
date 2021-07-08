package orbiter

import (
	"errors"
	"net/http"
	"time"

	orbcfg "github.com/caos/orbos/pkg/orb"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"

	"github.com/caos/orbos/internal/operator/common"
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

type ConfigureFunc func(orb orbcfg.Orb) error

func NoopConfigure(orb orbcfg.Orb) error {
	return nil
}

type QueryFunc func(nodeAgentsCurrent *common.CurrentNodeAgents, nodeAgentsDesired *common.DesiredNodeAgents, queried map[string]interface{}) (EnsureFunc, error)

type EnsureFunc func(pdf func(monitor mntr.Monitor) error) *EnsureResult

func NoopEnsure(_ func(monitor mntr.Monitor) error) *EnsureResult {
	return &EnsureResult{Done: true}
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

	treeDesired, err := gitClient.ReadTree(git.OrbiterFile)
	if err != nil {
		return nil, nil, nil, false, nil, nil, nil, err
	}
	treeCurrent := &tree.Tree{}

	query, destroy, configure, migrate, secrets, err := adapt(monitor, finished, treeDesired, treeCurrent)
	return query, destroy, configure, migrate, treeDesired, treeCurrent, secrets, err
}

const clearEachEnsuredIteration uint8 = 10

var clearCount uint8

func Takeoff(monitor mntr.Monitor, conf *Config, healthyChan chan bool) func() {

	return func() {

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
			if err = conf.GitClient.PushGitDesiredStates(monitor, "Desired state migrated", []git.GitDesiredState{{
				Desired: treeDesired,
				Path:    git.OrbiterFile,
			}}); err != nil {
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

		ensure, err := query(&currentNodeAgents.Current, &desiredNodeAgents.Spec.NodeAgents, nil)
		if err != nil {
			handleAdapterError(err)
			return
		}

		if err := conf.GitClient.Clone(); err != nil {
			monitor.Error(err)
			return
		}

		reconciledCurrentStateMsg := "Current state reconciled"

		if err := conf.GitClient.UpdateRemote(reconciledCurrentStateMsg, func() []git.File {
			return []git.File{marshalCurrentFiles()[0]}
		}); err != nil {
			monitor.Error(err)
			return
		}

		result := ensure(conf.GitClient.PushDesiredFunc(git.OrbiterFile, treeDesired))
		if result.Err != nil {
			handleAdapterError(result.Err)
			return
		}

		if result.Done {
			if clearCount < clearEachEnsuredIteration {
				clearCount++
			}
			monitor.Info("Desired state is ensured")
		} else {
			clearCount = 0
			monitor.Info("Desired state is not yet ensured")
		}

		conf.GitClient.UpdateRemote("Current state changed", func() []git.File {
			pushFiles := marshalCurrentFiles()
			if result.Done &&
				(len(currentNodeAgents.Current.NA) != len(desiredNodeAgents.Spec.NodeAgents.NA) || clearCount == clearEachEnsuredIteration) {
				clearCount = 0
				monitor.Info("Clearing node agents current states")
				pushFiles = append(pushFiles, git.File{
					Path:    "caos-internal/orbiter/node-agents-current.yml",
					Content: nil,
				})
			}
			return pushFiles
		})
	}
}
