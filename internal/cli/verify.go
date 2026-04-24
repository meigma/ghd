package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

func newVerifyCommand(options Options) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "verify [name|owner/repo/package|--all]",
		Short: "Re-verify active installed packages",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("verify accepts a target or --all, not both")
			}
			if !all && len(args) == 0 {
				return fmt.Errorf("verify target must be set")
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
			results, err := runtime.VerifyInstalled(cmd.Context(), app.VerifyInstalledRequest{
				Target:   target,
				All:      all,
				StateDir: cfg.StateDir,
			})
			writeVerifyResults(options, results)
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "verify all installed packages")
	return cmd
}

func writeVerifyResults(options Options, results []app.VerifyInstalledResult) {
	for _, result := range results {
		target := result.Repository + "/" + result.Package
		if result.Reason != "" {
			fmt.Fprintf(options.Out, "%s %s %s %s\n", target, result.Version, result.Status, result.Reason)
			continue
		}
		fmt.Fprintf(options.Out, "%s %s %s\n", target, result.Version, result.Status)
	}
}
