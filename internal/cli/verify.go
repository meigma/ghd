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
func newVerifyCommand(options Options) *cobra.Command {
	var all bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "verify [name|owner/repo/package|--all]",
		Short: "Re-verify active installed packages",
		Example: strings.TrimSpace(`
ghd verify package --state-dir ./state
ghd verify --all --state-dir ./state
ghd verify package --state-dir ./state --json
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectReadOnlyPresentationMode(options, jsonOutput)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}
			if all && len(args) > 0 {
				return errors.New("verify accepts a target or --all, not both")
			}
			if !all && len(args) == 0 {
				return errors.New("verify target must be set")
			}
			target := ""
			if len(args) == 1 {
				var err error
				target, err = parseVerifyTarget(args[0])
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
				if all || target == "" {
					status.Show("Re-verifying installed packages")
				} else {
					status.Show(fmt.Sprintf("Re-verifying %s", terminalSafeText(target)))
				}
			}
			results, err := runtime.VerifyInstalled(cmd.Context(), app.VerifyInstalledRequest{
				Target:   target,
				All:      all,
				StateDir: cfg.StateDir,
			})
			if status != nil {
				status.Clear()
			}
			if jsonOutput {
				if writeErr := writeVerifyResultsJSON(options, results); writeErr != nil {
					return writeErr
				}
			} else {
				if mode.richOutput {
					writeVerifyResultsTTY(options.Out, results, mode.color)
				} else {
					writeVerifyResults(options, results)
				}
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "verify all installed packages")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write verification results as JSON")
	return cmd
}

func writeVerifyResults(options Options, results []app.VerifyInstalledResult) {
	for _, result := range results {
		target := terminalSafeText(result.Repository + "/" + result.Package)
		if result.Reason != "" {
			fmt.Fprintf(
				options.Out,
				"%s %s %s %s\n",
				target,
				terminalSafeText(result.Version),
				result.Status,
				terminalSafeText(result.Reason),
			)
			continue
		}
		fmt.Fprintf(options.Out, "%s %s %s\n", target, terminalSafeText(result.Version), result.Status)
	}
}
