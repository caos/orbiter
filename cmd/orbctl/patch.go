// Inspired by https://samrapdev.com/capturing-sensitive-input-with-editor-in-golang-from-the-cli/

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/pkg/git"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func PatchCommand(getRv GetRootValues) *cobra.Command {

	cmd := &cobra.Command{
		Use:   "patch <filepath> [yamlpath]",
		Short: "Patch a yaml property",
		Args:  cobra.MinimumNArgs(1),
		Example: `Overwiting a file: orbctl file patch orbiter.yml --exact
Patching an edge property interactively: orbctl file patch orbiter.yml
Patching a node property non-interactively: orbctl file path orbiter.yml clusters.k8s --exact --file /path/to/my/cluster/definition.yml`,
	}
	flags := cmd.Flags()
	var (
		value string
		file  string
		stdin bool
		exact bool
	)
	flags.StringVar(&value, "value", "", "Content value")
	flags.StringVar(&file, "file", "", "File containing the content value")
	flags.BoolVar(&stdin, "stdin", false, "Read content value by stdin")
	flags.BoolVar(&exact, "exact", false, "Write the content exactly at the path given without further prompting")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {

		rv, err := getRv()
		if err != nil {
			return err
		}
		defer func() {
			err = rv.ErrFunc(err)
		}()

		if !rv.Gitops {
			return errors.New("patch command is only supported with the --gitops flag")
		}

		if err := initRepo(rv.OrbConfig, rv.GitClient); err != nil {
			return err
		}

		var path []string
		if len(args) > 1 {
			path = strings.Split(args[1], ".")
		}

		contentStr, err := content(value, file, stdin)
		if err != nil {
			return err
		}

		contentYaml := yamlTypedValue(contentStr)

		var result interface{}
		if len(path) == 0 && exact {
			result = contentYaml
		} else {
			structure := map[string]interface{}{}
			if err := yaml.Unmarshal(rv.GitClient.Read(args[0]), structure); err != nil {
				return err
			}
			if err := updateMap(structure, path, contentYaml, exact); err != nil {
				return err
			}
			result = structure
		}

		return rv.GitClient.UpdateRemote(fmt.Sprintf("Overwrite %s", args[0]), func() []git.File {
			return []git.File{{
				Path:    args[0],
				Content: common.MarshalYAML(result),
			}}
		})
	}

	return cmd
}

func updateMap(structure map[string]interface{}, path []string, value interface{}, exact bool) error {

	if len(path) == 1 && exact {
		structure[path[0]] = value
		return nil
	}

	path, nextStep := drillPath(path)

	if nextStep == "" {

		var keys []string
		for key := range structure {
			keys = append(keys, key)
		}

		var err error
		nextStep, err = prompt(keys)
		if err != nil {
			return err
		}
	}

	child, ok := structure[nextStep]
	if !ok {
		return fmt.Errorf("path element %s not found", nextStep)
	}

	drilled, err := drillContent(child, path, value, exact)
	if err != nil {
		return err
	}
	if !drilled {
		structure[nextStep] = value
	}

	return nil
}

func prompt(keys []string) (string, error) {
	var key string
	return key, survey.AskOne(&survey.Select{
		Message: "Select key:",
		Options: keys,
	}, &key, survey.WithValidator(survey.Required))
}

func updateSlice(slice []interface{}, path []string, value interface{}, exact bool) error {

	pos := func(pathNode string) (int, error) {
		idx, err := strconv.Atoi(pathNode)
		if err != nil {
			return -1, err
		}

		length := len(slice)
		if length < idx {
			return -1, fmt.Errorf("property has only %d elements", length)
		}
		return idx, nil
	}

	if len(path) == 1 && exact {
		idx, err := pos(path[0])
		if err != nil {
			return err
		}

		slice[idx] = value
		return nil
	}

	path, nextStep := drillPath(path)

	if nextStep == "" {
		var keys []string
		for key := range slice {
			keys = append(keys, strconv.Itoa(key))
		}

		var err error
		nextStep, err = prompt(keys)
		if err != nil {
			return err
		}
	}

	idx, err := pos(nextStep)

	drilled, err := drillContent(slice[idx], path, value, exact)
	if err != nil {
		return err
	}
	if !drilled {
		slice[idx] = value
	}

	return nil
}

func drillPath(path []string) ([]string, string) {
	var next string

	if len(path) > 0 {
		next = path[0]
		path = path[1:]
	}
	return path, next
}

func drillContent(child interface{}, path []string, value interface{}, exact bool) (bool, error) {

	switch typedNext := child.(type) {
	case map[string]interface{}:
		return true, updateMap(typedNext, path, value, exact)
	case []interface{}:
		return true, updateSlice(typedNext, path, value, exact)
	}

	if len(path) > 0 {
		return false, fmt.Errorf("invalid path %s", strings.Join(path, "."))
	}

	return false, nil
}

func yamlTypedValue(value string) interface{} {

	var out interface{}
	if err := yaml.Unmarshal([]byte(value), &out); err != nil {
		panic(err)
	}
	return out
}
