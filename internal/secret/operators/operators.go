package operators

import (
	"errors"
	"strings"

	"github.com/caos/orbos/pkg/labels"

	"github.com/caos/orbos/internal/api"
	boomapi "github.com/caos/orbos/internal/operator/boom/api"
	nwOrb "github.com/caos/orbos/internal/operator/networking/kinds/orb"
	orbiterOrb "github.com/caos/orbos/internal/operator/orbiter/kinds/orb"
	"github.com/caos/orbos/internal/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
)

const (
	boom       string = "boom"
	orbiter    string = "orbiter"
	networking string = "networking"
)

func GetAllSecretsFunc(orb *orb.Orb, binaryVersion *string) func(monitor mntr.Monitor, gitClient *git.Client) (map[string]*secret.Secret, map[string]*tree.Tree, error) {
	return func(monitor mntr.Monitor, gitClient *git.Client) (map[string]*secret.Secret, map[string]*tree.Tree, error) {
		allSecrets := make(map[string]*secret.Secret, 0)
		allTrees := make(map[string]*tree.Tree, 0)
		foundBoom, err := api.ExistsBoomYml(gitClient)
		if err != nil {
			return nil, nil, err
		}
		if foundBoom {
			boomYML, err := api.ReadBoomYml(gitClient)
			if err != nil {
				return nil, nil, err
			}
			allTrees[boom] = boomYML
			_, _, boomSecrets, _, _, err := boomapi.ParseToolset(boomYML)
			if err != nil {
				return nil, nil, err
			}

			if boomSecrets != nil && len(boomSecrets) > 0 {
				secret.AppendSecrets(boom, allSecrets, boomSecrets)
			}
		}

		foundOrbiter, err := api.ExistsOrbiterYml(gitClient)
		if err != nil {
			return nil, nil, err
		}
		if foundOrbiter {
			orbiterYML, err := api.ReadOrbiterYml(gitClient)
			if err != nil {
				return nil, nil, err
			}
			allTrees[orbiter] = orbiterYML

			_, _, _, _, orbiterSecrets, err := orbiterOrb.AdaptFunc(
				labels.NoopOperator("ORBOS"),
				orb,
				"",
				true,
				false,
				gitClient,
			)(monitor, make(chan struct{}), orbiterYML, &tree.Tree{})
			if err != nil {
				return nil, nil, err
			}

			if orbiterSecrets != nil && len(orbiterSecrets) > 0 {
				secret.AppendSecrets(orbiter, allSecrets, orbiterSecrets)
			}
		}

		foundNW, err := api.ExistsNetworkingYml(gitClient)
		if err != nil {
			return nil, nil, err
		}
		if foundNW {
			nwYML, err := api.ReadNetworkinglYml(gitClient)
			if err != nil {
				return nil, nil, err
			}
			allTrees[networking] = nwYML

			_, _, nwSecrets, err := nwOrb.AdaptFunc("", nil)(monitor, nwYML, nil)
			if err != nil {
				return nil, nil, err
			}
			if nwSecrets != nil && len(nwSecrets) > 0 {
				secret.AppendSecrets(networking, allSecrets, nwSecrets)
			}
		}

		return allSecrets, allTrees, nil
	}
}

func PushFunc() func(monitor mntr.Monitor, gitClient *git.Client, trees map[string]*tree.Tree, path string) error {
	return func(monitor mntr.Monitor, gitClient *git.Client, trees map[string]*tree.Tree, path string) error {
		operator := ""
		if strings.HasPrefix(path, orbiter) {
			operator = orbiter
		} else if strings.HasPrefix(path, boom) {
			operator = boom
		} else if strings.HasPrefix(path, networking) {
			operator = networking
		} else {
			return errors.New("Operator unknown")
		}

		desired, found := trees[operator]
		if !found {
			return errors.New("Operator file not found")
		}

		if operator == orbiter {
			return api.PushOrbiterDesiredFunc(gitClient, desired)(monitor)
		} else if operator == boom {
			return api.PushBoomDesiredFunc(gitClient, desired)(monitor)
		} else if operator == networking {
			return api.PushNetworkingDesiredFunc(gitClient, desired)(monitor)
		}

		return errors.New("Operator push function unknown")
	}
}
