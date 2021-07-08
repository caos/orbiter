package main

import (
	"github.com/spf13/cobra"
)

func FileCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "file <path> [command]",
		Short:   "Work with an orbs remote repository file",
		Example: `orbctl file <print|patch|edit|overwrite> orbiter.yml `,
	}
}
