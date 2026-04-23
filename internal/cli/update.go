package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

type updateOptions struct {
	storeDir string
	binDir   string
}

func newUpdateCommand(options Options) *cobra.Command {
	var update updateOptions
	cmd := &cobra.Command{
		Use:   "update name|owner/repo/package --store-dir DIR --bin-dir DIR",
		Short: "Update one active package to the latest eligible version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseUpdateTarget(args[0])
			if err != nil {
				return err
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := runtime.Update(cmd.Context(), app.UpdateRequest{
				Target:   target,
				StoreDir: cfg.StoreDir,
				BinDir:   cfg.BinDir,
				StateDir: cfg.StateDir,
			})
			if err != nil && !result.Updated {
				return err
			}
			if result.Updated {
				fmt.Fprintf(options.Err, "updated %s/%s@%s -> %s\n", result.Previous.Repository, result.Previous.Package, result.Previous.Version, result.Current.Version)
				for _, binary := range result.Binaries {
					fmt.Fprintf(options.Out, "binary %s\n", binary.LinkPath)
				}
			} else {
				fmt.Fprintf(options.Err, "already up to date %s/%s@%s\n", result.Current.Repository, result.Current.Package, result.Current.Version)
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&update.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&update.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}
