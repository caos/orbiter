package operators

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caos/orbos/internal/operator/boom/api/latest"

	orbiterOrb "github.com/caos/orbos/internal/operator/orbiter/kinds/orb"
	"github.com/caos/orbos/pkg/labels"

	boomcrd "github.com/caos/orbos/internal/api/boom"
	nwcrd "github.com/caos/orbos/internal/api/networking"
	boomapi "github.com/caos/orbos/internal/operator/boom/api"
	nwOrb "github.com/caos/orbos/internal/operator/networking/kinds/orb"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
	orbcfg "github.com/caos/orbos/pkg/orb"
	"github.com/caos/orbos/pkg/secret"
	"github.com/caos/orbos/pkg/tree"
)

func GetAllSecretsFunc(
	monitor mntr.Monitor,
	printLogs,
	gitops bool,
	gitClient *git.Client,
	k8sClient kubernetes.ClientInt,
	orb *orbcfg.Orb,
) func() (
	map[string]*secret.Secret,
	map[string]*secret.Existing,
	map[string]*tree.Tree,
	error,
) {
	return func() (
		map[string]*secret.Secret,
		map[string]*secret.Existing,
		map[string]*tree.Tree,
		error,
	) {
		return getAllSecrets(monitor, printLogs, gitops, gitClient, k8sClient, orb, true)
	}
}

func getAllSecrets(
	monitor mntr.Monitor,
	printLogs,
	gitops bool,
	gitClient *git.Client,
	k8sClient kubernetes.ClientInt,
	orb *orbcfg.Orb,
	disableIngestion bool,
) (
	map[string]*secret.Secret,
	map[string]*secret.Existing,
	map[string]*tree.Tree,
	error,
) {

	allSecrets := make(map[string]*secret.Secret, 0)
	allExisting := make(map[string]*secret.Existing, 0)
	allTrees := make(map[string]*tree.Tree, 0)

	if err := secret.GetOperatorSecrets(
		monitor,
		printLogs,
		gitops,
		gitClient,
		git.BoomFile,
		allTrees,
		allSecrets,
		allExisting,
		func() (*tree.Tree, error) { return boomcrd.ReadCRD(k8sClient) },
		func(t *tree.Tree) (map[string]*secret.Secret, map[string]*secret.Existing, bool, error) {
			toolset, migrate, _, _, err := boomapi.ParseToolset(t)
			if err != nil {
				return nil, nil, false, err
			}
			boomSecrets, boomExistingSecrets := latest.GetSecretsMap(toolset)
			return boomSecrets, boomExistingSecrets, migrate, nil
		},
	); err != nil {
		return nil, nil, nil, err
	}

	if gitops {
		if err := secret.GetOperatorSecrets(
			monitor,
			printLogs,
			gitops,
			gitClient,
			git.OrbiterFile,
			allTrees,
			allSecrets,
			allExisting,
			func() (*tree.Tree, error) { return nil, errors.New("ORBITER doesn't support crd mode") },
			func(t *tree.Tree) (map[string]*secret.Secret, map[string]*secret.Existing, bool, error) {
				_, _, _, migrate, orbiterSecrets, err := orbiterOrb.AdaptFunc(
					labels.NoopOperator("ORBOS"),
					orb,
					"",
					true,
					false,
					gitClient,
				)(monitor, make(chan struct{}), t, &tree.Tree{})
				return orbiterSecrets, nil, migrate, err
			},
		); err != nil {
			return nil, nil, nil, err
		}
	}

	if err := secret.GetOperatorSecrets(
		monitor,
		printLogs,
		gitops,
		gitClient,
		git.NetworkingFile,
		allTrees,
		allSecrets,
		allExisting,
		func() (*tree.Tree, error) { return nwcrd.ReadCRD(k8sClient) },
		func(t *tree.Tree) (map[string]*secret.Secret, map[string]*secret.Existing, bool, error) {
			_, _, nwSecrets, nwExisting, migrate, err := nwOrb.AdaptFunc(nil, false)(monitor, t, nil)
			return nwSecrets, nwExisting, migrate, err
		},
	); err != nil {
		return nil, nil, nil, err
	}

	if len(allSecrets) == 0 && len(allExisting) == 0 {
		return nil, nil, nil, errors.New("couldn't find any secrets")
	}

	return allSecrets, allExisting, allTrees, nil
}

func PushFunc(
	monitor mntr.Monitor,
	gitops bool,
	gitClient *git.Client,
	k8sClient kubernetes.ClientInt,
) func(
	trees map[string]*tree.Tree,
	path string,
) error {
	return func(
		trees map[string]*tree.Tree,
		path string,
	) error {
		return push(monitor, gitops, gitClient, k8sClient, trees, path)
	}
}

func push(
	monitor mntr.Monitor,
	gitops bool,
	gitClient *git.Client,
	k8sClient kubernetes.ClientInt,
	trees map[string]*tree.Tree,
	path string,
) error {
	var (
		applyCRDFunc func(*tree.Tree) error
		desiredFile  git.DesiredFile
	)
	if strings.HasPrefix(path, git.OrbiterFile.WOExtension()) {
		desiredFile = git.OrbiterFile
		applyCRDFunc = func(t *tree.Tree) error {
			panic(errors.New("ORBITER doesn't support CRD mode"))
		}
	} else if strings.HasPrefix(path, git.BoomFile.WOExtension()) {
		desiredFile = git.BoomFile
		applyCRDFunc = func(t *tree.Tree) error {
			return boomcrd.WriteCrd(k8sClient, t)
		}
	} else if strings.HasPrefix(path, git.NetworkingFile.WOExtension()) {
		desiredFile = git.NetworkingFile
		applyCRDFunc = func(t *tree.Tree) error {
			return nwcrd.WriteCrd(k8sClient, t)
		}
	} else {
		return errors.New("operator unknown")
	}

	desired, found := trees[desiredFile.WOExtension()]
	if !found {
		return fmt.Errorf("desired state not found for %s", desiredFile.WOExtension())
	}

	if gitops {
		return gitClient.PushDesiredFunc(desiredFile, desired)(monitor)
	}
	return applyCRDFunc(desired)
}
