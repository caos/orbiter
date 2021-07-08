package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/kataras/tablewriter"
	"github.com/landoop/tableprinter"
	"github.com/spf13/cobra"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/pkg/tree"
)

func ListCommand(getRv GetRootValues) *cobra.Command {
	var (
		column, context string
		cmd             = &cobra.Command{
			Use:   "list",
			Short: "List available machines",
			Long:  "List available machines",
			Args:  cobra.MaximumNArgs(1),
		}
	)

	flags := cmd.Flags()
	flags.StringVar(&column, "column", "", "Print this column only")
	flags.StringVar(&context, "context", "", "Print machines from this context only")

	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {
		rv, err := getRv()
		if err != nil {
			return err
		}
		defer func() {
			err = rv.ErrFunc(err)
		}()

		monitor := rv.Monitor
		orbConfig := rv.OrbConfig
		gitClient := rv.GitClient

		if !rv.Gitops {
			return errors.New("list command is only supported with the --gitops flag and a committed orbiter.yml")
		}

		return machines(monitor, gitClient, orbConfig, func(machineIDs []string, machines map[string]infra.Machine, _ *tree.Tree) error {

			printer := tableprinter.New(os.Stdout)
			printer.BorderTop, printer.BorderBottom = true, true
			printer.HeaderFgColor = tablewriter.FgYellowColor

			var (
				tail    bool
				headers []string
				rows    [][]string
				cellIdx = -1
			)

			for path, mach := range machines {
				ctx := path[:strings.LastIndex(path, ".")]
				if context == "" || context == ctx {
					v := reflect.ValueOf(mach).Elem()
					if !tail {
						headers = append([]string{"context"}, tableprinter.StructParser.ParseHeaders(v)...)
						if column != "" {
							for idx, h := range headers {
								if strings.Contains(strings.ToLower(h), strings.ToLower(column)) {
									cellIdx = idx
								}
							}
							if cellIdx == -1 {
								return fmt.Errorf("unknown column: %s", column)
							}
						}
						tail = true
					}

					cells, _ := tableprinter.StructParser.ParseRow(v)
					cells = append([]string{ctx}, cells...)

					if cellIdx > -1 {
						fmt.Println(cells[cellIdx])
						continue
					}
					rows = append(rows, cells)
				}
			}

			if cellIdx == -1 {
				printer.Render(headers, rows, nil, false)
			}

			return nil
		})
	}
	return cmd
}
