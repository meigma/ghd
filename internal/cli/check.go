package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

func newCheckCommand(options Options) *cobra.Command {
	var all bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "check [name|owner/repo/package|--all]",
		Short: "Check installed packages for available updates",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("check accepts a target or --all, not both")
			}
			target := ""
			if len(args) == 1 {
				var err error
				target, err = parseCheckTarget(args[0])
				if err != nil {
					return err
				}
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			results, err := runtime.CheckInstalled(cmd.Context(), app.CheckRequest{
				Target:   target,
				All:      all || len(args) == 0,
				StateDir: cfg.StateDir,
			})
			if jsonOutput {
				if writeErr := writeCheckResultsJSON(options, results); writeErr != nil {
					return writeErr
				}
			} else {
				writeCheckResults(options, results)
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
			fmt.Fprintf(options.Out, "%s %s %s %s\n", target, terminalSafeText(result.Version), result.Status, terminalSafeText(result.LatestVersion))
			continue
		}
		fmt.Fprintf(options.Out, "%s %s %s\n", target, terminalSafeText(result.Version), result.Status)
	}
}
