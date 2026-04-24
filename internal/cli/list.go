package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/verification"
)

func newListCommand(options Options) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list [owner/repo]",
		Short: "List available packages from the index or one repository",
		Example: strings.TrimSpace(`
ghd list --index-dir ./index
ghd list owner/repo
ghd --non-interactive list owner/repo
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectReadOnlyPresentationMode(options, jsonOutput)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}

			var repository verification.Repository
			if len(args) == 1 {
				var err error
				repository, err = parseListTarget(args[0])
				if err != nil {
					return err
				}
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if status != nil {
				if repository.IsZero() {
					status.Show("Loading indexed packages")
				} else {
					status.Show(fmt.Sprintf("Fetching packages from %s", terminalSafeText(repository.String())))
				}
			}
			results, err := runtime.ListPackages(cmd.Context(), app.PackageListRequest{
				Repository: repository,
				IndexDir:   cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			if status != nil {
				status.Clear()
			}
			if jsonOutput {
				return writePackageListJSON(options, results)
			}
			if mode.richOutput {
				writePackageListTTY(options.Out, results, repository, mode.color)
				return nil
			}
			writePackageList(options, results)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write package list as JSON")
	return cmd
}

func writePackageList(options Options, results []app.PackageListResult) {
	for _, result := range results {
		target := terminalSafeText(result.Repository.String() + "/" + result.PackageName)
		if len(result.Binaries) == 0 {
			fmt.Fprintln(options.Out, target)
			continue
		}
		fmt.Fprintf(options.Out, "%s %s\n", target, strings.Join(terminalSafeStrings(result.Binaries), ","))
	}
}
