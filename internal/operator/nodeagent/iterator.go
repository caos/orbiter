//go:generate goderive . -dedup -autoname

package nodeagent

import (
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
	monitor mntr.Monitor,
	gitClient *git.Client,
	nodeAgentCommit string,
	id string,
	firewallEnsurer FirewallEnsurer,
	networkingEnsurer NetworkingEnsurer,
	conv Converter,
	before func() error,
) func() {

	doQuery := prepareQuery(monitor, nodeAgentCommit, firewallEnsurer, networkingEnsurer, conv)

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

		if err := before(); err != nil {
			panic(err)
		}

		ensure, err := doQuery(*naDesired, curr)
		if err != nil {
			monitor.Error(err)
			return

		}
		readCurrent := func() common.NodeAgentsCurrentKind {
			current := common.NodeAgentsCurrentKind{}
			yaml.Unmarshal(gitClient.Read("caos-internal/orbiter/node-agents-current.yml"), &current)
			current.Kind = "nodeagent.caos.ch/NodeAgents"
			current.Version = "v0"
			return current
		}

		reconciledCurrentStateMsg := "Current state reconciled"
		if err := gitClient.UpdateRemote(mntr.CommitRecord([]*mntr.Field{{Key: "evt", Value: reconciledCurrentStateMsg}}), func() []git.File {

			current := readCurrent()
			current.Current.Set(id, curr)

			return []git.File{{
				Path:    "caos-internal/orbiter/node-agents-current.yml",
				Content: common.MarshalYAML(current),
			}}
		}); err != nil {
			monitor.Error(fmt.Errorf("commiting event \"%s\" failed: %w", reconciledCurrentStateMsg, err))
			return
		}

		monitor.Error(ensure())
	}
}
