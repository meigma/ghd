package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

type uninstallOptions struct {
	storeDir string
}

func newUninstallCommand(options Options) *cobra.Command {
	var uninstall uninstallOptions
	cmd := &cobra.Command{
		Use:   "uninstall name|owner/repo/package",
		Short: "Uninstall one active package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseUninstallTarget(args[0])
			if err != nil {
				return err
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := runtime.Uninstall(cmd.Context(), app.UninstallRequest{
				Target:   target,
				StoreDir: cfg.StoreDir,
				StateDir: cfg.StateDir,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(options.Err, "uninstalled %s/%s@%s\n", result.Record.Repository, result.Record.Package, result.Record.Version)
			return nil
		},
	}
	cmd.Flags().StringVar(&uninstall.storeDir, "store-dir", "", "managed store directory")
	return cmd
}
