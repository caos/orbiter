package gitcrd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/caos/orbos/internal/operator/boom/api"
	toolsetslatest "github.com/caos/orbos/internal/operator/boom/api/latest"
	bundleconfig "github.com/caos/orbos/internal/operator/boom/bundle/config"
	"github.com/caos/orbos/internal/operator/boom/crd"
	crdconfig "github.com/caos/orbos/internal/operator/boom/crd/config"
	"github.com/caos/orbos/internal/operator/boom/current"
	"github.com/caos/orbos/internal/operator/boom/gitcrd/config"
	"github.com/caos/orbos/internal/operator/boom/metrics"
	"github.com/caos/orbos/internal/utils/clientgo"
	"github.com/caos/orbos/internal/utils/helper"
	"github.com/caos/orbos/internal/utils/kubectl"
	"github.com/caos/orbos/internal/utils/kustomize"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type GitCrd struct {
	crd              *crd.Crd
	git              *git.Client
	crdDirectoryPath string
	status           error
	monitor          mntr.Monitor
}

func New(conf *config.Config) *GitCrd {

	monitor := conf.Monitor.WithFields(map[string]interface{}{
		"type": "gitcrd",
	})

	monitor.Info("New GitCRD")

	gitCrd := &GitCrd{
		crdDirectoryPath: conf.CrdDirectoryPath,
		git:              conf.Git,
		monitor:          monitor,
	}

	crdConf := &crdconfig.Config{
		Monitor: monitor,
	}

	gitCrd.crd = crd.New(crdConf)

	return gitCrd
}

func (c *GitCrd) Clone(url string, key []byte) error {
	err := c.git.Configure(url, key)
	if err != nil {
		c.monitor.Error(err)
		return err
	}

	err = c.git.Clone()
	if err != nil {
		c.monitor.Error(err)
		metrics.FailedGitClone(url)
		return err
	}
	metrics.SuccessfulGitClone(url)
	return nil
}

func (c *GitCrd) GetStatus() error {
	return c.status
}

func (c *GitCrd) SetBackStatus() {
	c.crd.SetBackStatus()
	c.status = nil
}

func (c *GitCrd) SetBundle(conf *bundleconfig.Config) {
	if c.status != nil {
		return
	}

	toolsetCRD, err := c.getCrdMetadata()
	if err != nil {
		c.status = err
		return
	}

	monitor := c.monitor.WithFields(map[string]interface{}{
		"CRD": toolsetCRD.Metadata.Name,
	})

	bundleConf := &bundleconfig.Config{
		Monitor:           monitor,
		CrdName:           toolsetCRD.Metadata.Name,
		BundleName:        conf.BundleName,
		BaseDirectoryPath: conf.BaseDirectoryPath,
		Templator:         conf.Templator,
		Orb:               conf.Orb,
	}

	c.crd.SetBundle(bundleConf)
	c.status = c.crd.GetStatus()
}

func (c *GitCrd) CleanUp() {
	if c.status != nil {
		return
	}

	c.status = os.RemoveAll(c.crdDirectoryPath)
}

func (c *GitCrd) GetRepoURL() string {
	return c.git.GetURL()
}

func (c *GitCrd) Reconcile(currentResourceList []*clientgo.Resource) {
	if c.status != nil {
		return
	}

	monitor := c.monitor.WithFields(map[string]interface{}{
		"action": "reconiling",
	})

	toolsetCRD, err := c.getCrdContent()
	if err != nil {
		c.status = err
		return
	}

	// pre-steps
	if toolsetCRD.Spec.PreApply != nil {
		preapplymonitor := monitor.WithField("application", "preapply")
		preapplymonitor.Info("Start")
		if err := c.applyFolder(preapplymonitor, toolsetCRD.Spec.PreApply, toolsetCRD.Spec.ForceApply); err != nil {
			c.status = errors.Wrap(err, "Preapply failed")
			return
		}
		preapplymonitor.Info("Done")
	}

	c.crd.Reconcile(currentResourceList, toolsetCRD, true)
	err = c.crd.GetStatus()
	if err != nil {
		c.status = err
		return
	}

	// post-steps
	if toolsetCRD.Spec.PostApply != nil {
		preapplymonitor := monitor.WithField("application", "postapply")
		preapplymonitor.Info("Start")
		if err := c.applyFolder(monitor, toolsetCRD.Spec.PostApply, toolsetCRD.Spec.ForceApply); err != nil {
			c.status = errors.Wrap(err, "Postapply failed")
			return
		}
		preapplymonitor.Info("Done")
	}
}

func (c *GitCrd) getCrdMetadata() (*toolsetslatest.ToolsetMetadata, error) {
	toolsetCRD := &toolsetslatest.ToolsetMetadata{}
	err := c.git.ReadYamlIntoStruct("boom.yml", toolsetCRD)
	if err != nil {
		return nil, errors.Wrap(err, "Error while unmarshaling boom.yml to struct")
	}

	return toolsetCRD, nil

}

func (c *GitCrd) getCrdContent() (*toolsetslatest.Toolset, error) {
	desiredTree, err := c.git.ReadTree(git.BoomFile)
	if err != nil {
		return nil, err
	}

	desiredKind, _, _, _, err := api.ParseToolset(desiredTree)
	if err != nil {
		return nil, errors.Wrap(err, "parsing desired state failed")
	}
	desiredTree.Parsed = desiredKind

	return desiredKind, nil
}

func (c *GitCrd) WriteBackCurrentState(currentResourceList []*clientgo.Resource) {
	if c.status != nil {
		return
	}

	content, err := yaml.Marshal(current.ResourcesToYaml(currentResourceList))
	if err != nil {
		c.status = err
		return
	}

	toolsetCRD, err := c.getCrdContent()
	if err != nil {
		c.status = err
		return
	}

	currentFolder := toolsetCRD.Spec.CurrentStateFolder
	if currentFolder == "" {
		currentFolder = filepath.Join("caos-internal", "boom")
	}

	file := git.File{
		Path:    filepath.Join(currentFolder, "current.yaml"),
		Content: content,
	}

	c.status = c.git.UpdateRemote("current state changed", func() []git.File { return []git.File{file} })
}

func (c *GitCrd) applyFolder(monitor mntr.Monitor, apply *toolsetslatest.Apply, force bool) error {
	if apply.Folder == "" {
		monitor.Info("No folder provided")
		return nil
	}

	err := helper.CopyFolderToLocal(c.git, c.crdDirectoryPath, apply.Folder)
	if err != nil {
		return err
	}

	localFolder := filepath.Join(c.crdDirectoryPath, apply.Folder)
	if !helper.FolderExists(localFolder) {
		return errors.New("Folder is nonexistent")
	}

	if empty, err := helper.FolderEmpty(localFolder); empty == true || err != nil {
		monitor.Info("Provided folder is empty")
		return nil
	}

	if err := useFolder(monitor, apply.Deploy, localFolder, force); err != nil {
		return err
	}
	return nil
}

func useFolder(monitor mntr.Monitor, deploy bool, folderPath string, force bool) error {

	files, err := getFilesInDirectory(folderPath)
	if err != nil {
		return err
	}
	kustomizeFile := false
	for _, file := range files {
		if strings.HasSuffix(file, "kustomization.yaml") {
			kustomizeFile = true
			break
		}
	}

	if kustomizeFile {
		command, err := kustomize.New(folderPath)
		if err != nil {
			return err
		}
		command = command.Apply(force)
		if !deploy {
			command = command.Delete()
		}
		return helper.Run(monitor, command.Build())
	} else {
		return recursiveFolder(monitor, folderPath, deploy, force)
	}
}

func recursiveFolder(monitor mntr.Monitor, folderPath string, deploy, force bool) error {
	command := kubectl.NewApply(folderPath).Build()
	if force {
		command = kubectl.NewApply(folderPath).Force().Build()
	}
	if !deploy {
		command = kubectl.NewDelete(folderPath).Build()
	}

	folders, err := getDirsInDirectory(folderPath)
	if err != nil {
		return err
	}

	for _, folder := range folders {
		if folderPath != folder {
			if err := recursiveFolder(monitor, filepath.Join(folderPath, folder), deploy, force); err != nil {
				return err
			}
		}
	}
	return helper.Run(monitor, command)
}

func getFilesInDirectory(dirPath string) ([]string, error) {
	files := make([]string, 0)

	infos, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	for _, info := range infos {
		if !info.IsDir() {
			files = append(files, filepath.Join(dirPath, info.Name()))
		}
	}

	return files, err
}

func getDirsInDirectory(dirPath string) ([]string, error) {
	dirs := make([]string, 0)

	infos, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	for _, info := range infos {
		if info.IsDir() {
			dirs = append(dirs, filepath.Join(dirPath, info.Name()))
		}
	}
	return dirs, err
}
