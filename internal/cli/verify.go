package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

func newVerifyCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "verify name|owner/repo/package",
		Short: "Re-verify one active installed package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseVerifyTarget(args[0])
			if err != nil {
				return err
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := runtime.VerifyInstalled(cmd.Context(), app.VerifyInstalledRequest{
				Target:   target,
				StateDir: cfg.StateDir,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(options.Err, "verified %s/%s@%s\n", result.Repository, result.Package, result.Version)
			return nil
		},
	}
}
