package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/yaml.v3"

	"github.com/pkg/errors"

	"github.com/caos/orbos/mntr"
	"golang.org/x/crypto/ssh"
	"gopkg.in/src-d/go-billy.v4"
	"gopkg.in/src-d/go-billy.v4/memfs"
	gogit "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	gitssh "gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
	"gopkg.in/src-d/go-git.v4/storage/memory"
)

const (
	writeCheckTag = "writecheck"
)

type Client struct {
	monitor   mntr.Monitor
	ctx       context.Context
	committer string
	email     string
	auth      *gitssh.PublicKeys
	repo      *gogit.Repository
	fs        billy.Filesystem
	workTree  *gogit.Worktree
	progress  io.Writer
	repoURL   string
	cloned    bool
}

func New(monitor mntr.Monitor, committer, email string) *Client {
	newClient := &Client{
		committer: committer,
		email:     email,
		monitor:   monitor,
	}

	if monitor.IsVerbose() {
		newClient.progress = os.Stdout
	}
	return newClient
}

func (g *Client) GetURL() string {
	return g.repoURL
}

func (g *Client) Configure(ctx context.Context, repoURL string, deploykey []byte) error {
	g.ctx = ctx
	signer, err := ssh.ParsePrivateKey(deploykey)
	if err != nil {
		return errors.Wrap(err, "parsing deployment key failed")
	}

	g.repoURL = repoURL
	g.monitor = g.monitor.WithField("repository", repoURL)

	g.auth = &gitssh.PublicKeys{
		User:   "git",
		Signer: signer,
	}

	// TODO: Fix
	g.auth.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	return nil
}

func (g *Client) Check() error {
	if !g.cloned {
		return nil
	}
	if err := g.readCheck(); err != nil {
		return err
	}

	return g.writeCheck()
}

func (g *Client) readCheck() error {
	rem := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{g.repoURL},
	})

	// We can then use every Remote functions to retrieve wanted information
	_, err := rem.List(&gogit.ListOptions{
		Auth: g.auth,
	})
	if err != nil {
		return errors.Wrap(err, "Read check failed")
	}

	g.monitor.Info("Read check success")
	return nil
}

func (g *Client) writeCheck() error {

	head, err := g.repo.Head()
	if err != nil {
		return errors.Wrap(err, "Failed to get head")
	}
	localWriteCheckTag := strings.Join([]string{writeCheckTag, g.committer}, "-")

	ref, createErr := g.repo.CreateTag(localWriteCheckTag, head.Hash(), nil)
	if createErr == gogit.ErrTagExists {
		if ref, err = g.repo.Tag(localWriteCheckTag); err != nil {
			return err
		}
	}

	if createErr != nil {
		return errors.Wrap(createErr, "Write-check failed")
	}

	if pushErr := g.repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec("+" + ref.Name() + ":" + ref.Name()),
		},
		Auth: g.auth,
	}); pushErr != nil && pushErr == gogit.NoErrAlreadyUpToDate {
		return errors.Wrap(pushErr, "Write-check failed")
	}

	g.monitor.Debug("Write check tag created")

	if deleteErr := g.repo.DeleteTag(localWriteCheckTag); deleteErr != nil && deleteErr != gogit.ErrTagNotFound {
		return errors.Wrap(err, "Write-check cleanup delete tag failed")
	}

	if err := g.repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec(":" + ref.Name()),
		},
		Auth: g.auth,
	}); err != nil {
		return errors.Wrap(err, "Write-check cleanup failed")
	}

	g.monitor.Debug("Write check tag cleaned up")
	g.monitor.Info("Write check success")
	return nil
}

func (g *Client) Clone() (err error) {
	for i := 0; i < 10; i++ {
		if err = g.clone(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}

func (g *Client) clone() error {

	g.fs = memfs.New()

	g.monitor.Debug("Cloning")
	var err error
	g.repo, err = gogit.CloneContext(g.ctx, memory.NewStorage(), g.fs, &gogit.CloneOptions{
		URL:          g.repoURL,
		Auth:         g.auth,
		SingleBranch: true,
		Depth:        1,
		Progress:     g.progress,
	})
	if err != nil {
		return errors.Wrapf(err, "cloning repository from %s failed", g.repoURL)
	}
	g.monitor.Debug("Cloned")

	g.workTree, err = g.repo.Worktree()
	if err != nil {
		panic(err)
	}

	g.cloned = true

	return nil
}

func (g *Client) Read(path string) []byte {

	readmonitor := g.monitor.WithFields(map[string]interface{}{
		"path": path,
	})
	readmonitor.Debug("Reading file")
	file, err := g.fs.Open(path)
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			return make([]byte, 0)
		}
		panic(err)
	}
	defer file.Close()
	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		panic(err)
	}
	if readmonitor.IsVerbose() {
		readmonitor.Debug("File read")
		fmt.Println(string(fileBytes))
	}
	return fileBytes
}

func (g *Client) ReadYamlIntoStruct(path string, struc interface{}) error {
	data := g.Read(path)

	return errors.Wrapf(yaml.Unmarshal(data, struc), "Error while unmarshaling yaml %s to struct", path)
}

func (g *Client) ExistsFolder(path string) (bool, error) {
	monitor := g.monitor.WithFields(map[string]interface{}{
		"path": path,
	})
	monitor.Debug("Reading folder")
	_, err := g.fs.ReadDir(path)
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			return false, nil
		}
		return false, errors.Wrapf(err, "opening %s from worktree failed", path)
	}

	return true, nil
}

func (g *Client) EmptyFolder(path string) (bool, error) {
	monitor := g.monitor.WithFields(map[string]interface{}{
		"path": path,
	})
	monitor.Debug("Reading folder")
	files, err := g.fs.ReadDir(path)
	if err != nil {
		return false, errors.Wrapf(err, "opening %s from worktree failed", path)
	}
	if len(files) == 0 {
		return true, nil
	}
	return false, nil
}

func (g *Client) ReadFolder(path string) (map[string][]byte, []string, error) {
	monitor := g.monitor.WithFields(map[string]interface{}{
		"path": path,
	})
	monitor.Debug("Reading folder")
	dirBytes := make(map[string][]byte, 0)
	files, err := g.fs.ReadDir(path)
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			return make(map[string][]byte, 0), nil, nil
		}
		return nil, nil, errors.Wrapf(err, "opening %s from worktree failed", path)
	}
	subdirs := make([]string, 0)
	for _, file := range files {
		if !file.IsDir() {
			filePath := filepath.Join(path, file.Name())
			fileBytes := g.Read(filePath)
			dirBytes[file.Name()] = fileBytes
		} else {
			subdirs = append(subdirs, file.Name())
		}
	}

	if monitor.IsVerbose() {
		monitor.Debug("Folder read")
		fmt.Println(dirBytes)
	}
	return dirBytes, subdirs, nil
}

type File struct {
	Path    string
	Content []byte
}

func (g *Client) StageAndCommit(msg string, files ...File) (bool, error) {
	if g.stage(files...) {
		return false, nil
	}

	return true, g.Commit(msg)
}

func (g *Client) UpdateRemote(msg string, files ...File) error {
	if err := g.Clone(); err != nil {
		return errors.Wrap(err, "recloning before committing changes failed")
	}

	changed, err := g.StageAndCommit(msg, files...)
	if err != nil {
		return err
	}

	if !changed {
		g.monitor.Info("No changes")
		return nil
	}

	return g.Push()
}

func (g *Client) stage(files ...File) bool {
	for _, f := range files {
		updatemonitor := g.monitor.WithFields(map[string]interface{}{
			"path": f.Path,
		})

		updatemonitor.Debug("Overwriting local index")

		file, err := g.fs.Create(f.Path)
		if err != nil {
			panic(err)
		}
		//noinspection GoDeferInLoop
		defer file.Close()

		if _, err := io.Copy(file, bytes.NewReader(f.Content)); err != nil {
			panic(err)
		}

		_, err = g.workTree.Add(f.Path)
		if err != nil {
			panic(err)
		}
	}

	status, err := g.workTree.Status()
	if err != nil {
		panic(err)
	}

	return status.IsClean()
}

func (g *Client) Commit(msg string) error {

	if _, err := g.workTree.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  g.committer,
			Email: g.email,
			When:  time.Now(),
		},
	}); err != nil {
		return errors.Wrap(err, "committing changes failed")
	}
	g.monitor.Debug("Changes commited")
	return nil
}

func (g *Client) Push() error {

	err := g.repo.PushContext(g.ctx, &gogit.PushOptions{
		RemoteName: "origin",
		//			RefSpecs:   refspecs,
		Auth:     g.auth,
		Progress: g.progress,
	})
	if err != nil {
		return errors.Wrap(err, "pushing repository failed")
	}

	g.monitor.Info("Repository pushed")
	return nil
}
