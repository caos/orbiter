package orb

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caos/orbos/internal/ssh"
	"github.com/caos/orbos/internal/stores/github"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"

	"github.com/caos/orbos/pkg/secret"

	"github.com/caos/orbos/internal/helpers"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

var alphanum = regexp.MustCompile("[^a-zA-Z0-9]+")

type Orb struct {
	id        string `yaml:"-"`
	Path      string `yaml:"-"`
	URL       string
	Repokey   string
	Masterkey string
}

func (o *Orb) IsConnectable() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("repository is not connectable: %w", err)
		}
	}()
	if o.URL == "" {
		err = helpers.Concat(err, errors.New("repository url is missing"))
	}

	if o.Repokey == "" {
		err = helpers.Concat(err, errors.New("repository key is missing"))
	}
	return err
}

func IsComplete(o *Orb) (err error) {

	defer func() {
		if err != nil {
			err = fmt.Errorf("orbconfig is incomplete: %w", err)
		}
	}()

	if o == nil {
		return errors.New("path not provided")
	}

	if o.Masterkey == "" {
		err = helpers.Concat(err, errors.New("master key is missing"))
	}

	if o.Path == "" {
		err = helpers.Concat(err, errors.New("file path is missing"))
	}

	return helpers.Concat(err, o.IsConnectable())
}

func ParseOrbConfig(orbConfigPath string) (*Orb, error) {

	gitOrbConfig, err := ioutil.ReadFile(orbConfigPath)

	if err != nil {
		return nil, errors.Wrap(err, "unable to read orbconfig")
	}

	orb := &Orb{}
	if err := yaml.Unmarshal(gitOrbConfig, orb); err != nil {
		return nil, errors.Wrap(err, "unable to parse orbconfig")
	}

	orb.Path = orbConfigPath
	secret.Masterkey = orb.Masterkey
	return orb, nil
}

func (o *Orb) writeBackOrbConfig() error {
	data, err := yaml.Marshal(o)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(o.Path, data, os.ModePerm)
}

func Reconfigure(ctx context.Context, monitor mntr.Monitor, orbConfig *Orb, newRepoURL, newMasterKey string, gitClient *git.Client, clientID, clientSecret string) (err error) {
	if orbConfig.URL == "" && newRepoURL == "" {
		return errors.New("repository url is neighter passed by flag repourl nor written in orbconfig")
	}

	// TODO: Remove?
	if orbConfig.URL != "" && newRepoURL != "" && orbConfig.URL != newRepoURL {
		return fmt.Errorf("repository url %s is not reconfigurable", orbConfig.URL)
	}

	if orbConfig.Masterkey == "" && newMasterKey == "" {
		return errors.New("master key is neighter passed by flag masterkey nor written in orbconfig")
	}

	var changes bool
	if newMasterKey != "" {
		monitor.Info("Changing masterkey in current orbconfig")
		if orbConfig.Masterkey == "" {
			secret.Masterkey = newMasterKey
		}
		orbConfig.Masterkey = newMasterKey
		changes = true
	}
	if newRepoURL != "" {
		monitor.Info("Changing repository url in current orbconfig")
		defer func() {
			if err == nil {
				monitor.WithField("url", newRepoURL).CaptureMessage("New Repository URL configured")
			}
		}()
		orbConfig.URL = newRepoURL
		changes = true
	}

	configureGit := func() error {
		if err := gitClient.Configure(orbConfig.URL, []byte(orbConfig.Repokey)); err != nil {
			return err
		}
		if err := gitClient.Clone(); err != nil {
			return err
		}
		return gitClient.Check()
	}

	// If the repokey already has read/write permissions, don't generate a new one.
	// This ensures git providers other than github keep being supported
	if err := configureGit(); err != nil {

		monitor.Info("Starting connection with git-repository")

		dir := filepath.Dir(orbConfig.Path)

		deployKeyPrivLocal, deployKeyPub, err := ssh.Generate()
		if err != nil {
			panic(errors.New("failed to generate ssh key for deploy key"))
		}
		g := github.New(monitor).LoginOAuth(ctx, dir, clientID, clientSecret)
		if err := g.GetStatus(); err != nil {
			return fmt.Errorf("github oauth login failed: %w", err)
		}
		repo, err := g.GetRepositorySSH(orbConfig.URL)
		if err != nil {
			return errors.New("failed to get github repository")
		}

		if err := g.EnsureNoDeployKey(repo).GetStatus(); err != nil {
			monitor.Error(errors.New("failed to clear deploy keys in repository"))
		}

		if err := g.CreateDeployKey(repo, deployKeyPub).GetStatus(); err != nil {
			return errors.New("failed to create deploy keys in repository")
		}
		orbConfig.Repokey = deployKeyPrivLocal

		if err := configureGit(); err != nil {
			return err
		}
		changes = true
	}

	if changes {
		monitor.Info("Writing local orbconfig")
		if err := orbConfig.writeBackOrbConfig(); err != nil {
			monitor.Info("Failed to change local configuration")
			return err
		}
	}

	return nil
}

func (o *Orb) ID() (string, error) {

	if err := IsComplete(o); err != nil {
		return "", err
	}

	if o.id != "" {
		return o.id, nil
	}

	o.id = alphanum.ReplaceAllString(strings.TrimSuffix(strings.TrimPrefix(o.URL, "git@"), ".git"), "-")
	return o.id, nil
}
