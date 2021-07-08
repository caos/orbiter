// Inspired by https://samrapdev.com/capturing-sensitive-input-with-editor-in-golang-from-the-cli/

package main

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/term"

	"github.com/caos/orbos/pkg/git"
)

func EditCommand(getRv GetRootValues) *cobra.Command {
	return &cobra.Command{
		Use:     "edit <path>",
		Short:   "Edit the file in your favorite text editor",
		Args:    cobra.ExactArgs(1),
		Example: `orbctl file edit desired.yml`,
		RunE: func(cmd *cobra.Command, args []string) (err error) {

			rv, err := getRv()
			if err != nil {
				return err
			}
			defer func() {
				err = rv.ErrFunc(err)
			}()

			orbConfig := rv.OrbConfig
			gitClient := rv.GitClient

			if !rv.Gitops {
				return errors.New("edit command is only supported with the --gitops flag")
			}

			if err := initRepo(orbConfig, gitClient); err != nil {
				return err
			}

			edited, err := captureInputFromEditor(GetPreferredEditorFromEnvironment, bytes.NewReader(gitClient.Read(args[0])))
			if err != nil {
				panic(err)
			}

			return gitClient.UpdateRemote("File written by orbctl", func() []git.File {
				return []git.File{{
					Path:    args[0],
					Content: edited,
				}}
			})
		},
	}
}

// DefaultEditor is vim because we're adults ;)
const DefaultEditor = "vim"

// PreferredEditorResolver is a function that returns an editor that the user
// prefers to use, such as the configured `$EDITOR` environment variable.
type PreferredEditorResolver func() string

// GetPreferredEditorFromEnvironment returns the user's editor as defined by the
// `$EDITOR` environment variable, or the `DefaultEditor` if it is not set.
func GetPreferredEditorFromEnvironment() string {
	editor := os.Getenv("EDITOR")

	if editor == "" {
		return DefaultEditor
	}

	return editor
}

func resolveEditorArguments(executable string, filename string) []string {
	args := []string{filename}

	if strings.Contains(executable, "Visual Studio Code.app") {
		args = append([]string{"--wait"}, args...)
	}

	if strings.Contains(executable, "vim") {
		args = append([]string{"--not-a-term", "-c", "set nowrap"}, args...)
	}

	// Other common editors

	return args
}

// openFileInEditor opens filename in a text editor.
func openFileInEditor(filename string, resolveEditor PreferredEditorResolver) error {
	// Get the full executable path for the editor.
	executable, err := exec.LookPath(resolveEditor())
	if err != nil {
		return err
	}

	cmd := exec.Command(executable, resolveEditorArguments(executable, filename)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return (term.TTY{In: os.Stdin, TryDev: true}).Safe(cmd.Run)
}

// captureInputFromEditor opens a temporary file in a text editor and returns
// the written bytes on success or an error on failure. It handles deletion
// of the temporary file behind the scenes.
func captureInputFromEditor(resolveEditor PreferredEditorResolver, content io.Reader) ([]byte, error) {
	file, err := ioutil.TempFile(os.TempDir(), "*")
	if err != nil {
		return []byte{}, err
	}

	filename := file.Name()

	// Defer removal of the temporary file in case any of the next steps fail.
	defer os.Remove(filename)

	if _, err := io.Copy(file, content); err != nil {
		return []byte{}, err
	}

	if err = file.Close(); err != nil {
		return []byte{}, err
	}

	if err = openFileInEditor(filename, resolveEditor); err != nil {
		return []byte{}, err
	}

	return ioutil.ReadFile(filename)
}
