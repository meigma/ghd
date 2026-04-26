package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

//nolint:gocognit // Cobra command construction is mostly declarative CLI wiring.
func newCheckCommand(options Options) *cobra.Command {
	var all bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "check [name|owner/repo/package|--all]",
		Short: "Check installed packages for available updates",
		Example: strings.TrimSpace(`
ghd check --state-dir ./state
ghd check foo --state-dir ./state
ghd --non-interactive check --all --state-dir ./state
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return errors.New("check accepts a target or --all, not both")
			}
			target := ""
			if len(args) == 1 {
				var err error
				target, err = parseCheckTarget(args[0])
				if err != nil {
					return err
				}
			}
			mode := detectReadOnlyPresentationMode(options, jsonOutput)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if status != nil {
				status.Show("Checking installed packages for updates")
			}
			results, err := runtime.CheckInstalled(cmd.Context(), app.CheckRequest{
				Target:   target,
				All:      all || len(args) == 0,
				StateDir: cfg.StateDir,
			})
			if status != nil {
				status.Clear()
			}
			if jsonOutput {
				if writeErr := writeCheckResultsJSON(options, results); writeErr != nil {
					return writeErr
				}
			} else {
				if mode.richOutput {
					writeCheckResultsTTY(options.Out, results, mode.color)
				} else {
					writeCheckResults(options, results)
				}
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "check all installed packages")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write update checks as JSON")
	return cmd
}

func writeCheckResults(options Options, results []app.CheckResult) {
	for _, result := range results {
		target := terminalSafeText(result.Repository + "/" + result.Package)
		if result.LatestVersion != "" {
			fmt.Fprintf(
				options.Out,
				"%s %s %s %s\n",
				target,
				terminalSafeText(result.Version),
				result.Status,
				terminalSafeText(result.LatestVersion),
			)
			continue
		}
		fmt.Fprintf(options.Out, "%s %s %s\n", target, terminalSafeText(result.Version), result.Status)
	}
}
