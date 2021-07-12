//go:generate goderive . -dedup -autoname

package nodeagent

import (
	"context"
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v3"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
)

type Rebooter interface {
	Reboot() error
}

type event struct {
	commit  string
	current *common.NodeAgentCurrent
}

func RepoKey() ([]byte, error) {

	path := "/var/orbiter/repo-key"
	key, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("repokey not found at %s: %w", path, err)
	}

	return key, err
}

func Iterator(
	ctx context.Context,
	monitor mntr.Monitor,
	gitClient *git.Client,
	nodeAgentCommit string,
	id string,
	firewallEnsurer FirewallEnsurer,
	networkingEnsurer NetworkingEnsurer,
	conv Converter,
	before func() error,
) func() {

	doQuery := prepareQuery(ctx, monitor, nodeAgentCommit, firewallEnsurer, networkingEnsurer, conv)

	return func() {

		repoKey, err := RepoKey()
		if err != nil {
			monitor.Error(err)
			return
		}

		repoURL, err := ioutil.ReadFile("/var/orbiter/repo-url")
		if err != nil {
			monitor.Error(err)
			return
		}

		if err := gitClient.Configure(string(repoURL), repoKey); err != nil {
			monitor.Error(err)
			return
		}

		if err := gitClient.Clone(); err != nil {
			monitor.Error(err)
			return
		}

		desired := common.NodeAgentsDesiredKind{}
		if err := yaml.Unmarshal(gitClient.Read("caos-internal/orbiter/node-agents-desired.yml"), &desired); err != nil {
			monitor.Error(err)
			return
		}

		naDesired, ok := desired.Spec.NodeAgents.Get(id)
		if !ok {
			monitor.Error(fmt.Errorf("no desired state for node agent with id %s found", id))
			return
		}

		if nodeAgentCommit != "debug" && desired.Spec.Commit != nodeAgentCommit {
			monitor.WithFields(map[string]interface{}{
				"desired": desired.Spec.Commit,
				"current": nodeAgentCommit,
			}).Info("Node Agent is on the wrong commit")
			return
		}

		curr := &common.NodeAgentCurrent{}

		events := make([]*event, 0)
		monitor.OnChange = mntr.Concat(func(evt string, fields map[string]string) {
			clone := *curr
			events = append(events, &event{
				commit:  mntr.CommitRecord(mntr.AggregateCommitFields(fields)),
				current: &clone,
			})
		}, monitor.OnChange)

		if err := before(); err != nil {
			panic(err)
		}

		/*query := func() (func() error, error) {
			return doQuery(*naDesired, curr)
		}

		ensure, err := QueryFuncGoroutine(query)*/
		ensure, err := doQuery(*naDesired, curr)
		if err != nil {
			monitor.Error(err)
			return

		}
		readCurrent := func() common.NodeAgentsCurrentKind {
			if err := gitClient.Clone(); err != nil {
				panic(err)
			}
			current := common.NodeAgentsCurrentKind{}
			yaml.Unmarshal(gitClient.Read("caos-internal/orbiter/node-agents-current.yml"), &current)
			current.Kind = "nodeagent.caos.ch/NodeAgents"
			current.Version = "v0"
			return current
		}

		current := readCurrent()
		current.Current.Set(id, curr)

		reconciledCurrentStateMsg := "Current state reconciled"
		reconciledCurrent, err := gitClient.StageAndCommit(mntr.CommitRecord([]*mntr.Field{{Key: "evt", Value: reconciledCurrentStateMsg}}), git.File{
			Path:    "caos-internal/orbiter/node-agents-current.yml",
			Content: common.MarshalYAML(current),
		})
		if err != nil {
			monitor.Error(fmt.Errorf("commiting event \"%s\" failed: %s", reconciledCurrentStateMsg, err.Error()))
			return
		}

		if reconciledCurrent {
			monitor.Error(gitClient.Push())
		}

		if err := ensure(); err != nil {
			monitor.Error(err)
			return
		}

		if events != nil && len(events) > 0 {
			current := readCurrent()

			for _, event := range events {
				current.Current.Set(id, event.current)
				changed, err := gitClient.StageAndCommit(event.commit, git.File{
					Path:    "caos-internal/orbiter/node-agents-current.yml",
					Content: common.MarshalYAML(current),
				})
				if err != nil {
					monitor.Error(fmt.Errorf("commiting event \"%s\" failed: %s", event.commit, err.Error()))
					return
				}
				if !changed {
					monitor.Error(fmt.Errorf("event has no effect:", event.commit))
					return
				}
			}
			monitor.Error(gitClient.Push())
		}
	}
}
