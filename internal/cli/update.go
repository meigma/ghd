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
	var all bool
	var update updateOptions
	cmd := &cobra.Command{
		Use:   "update [name|owner/repo/package|--all] --store-dir DIR --bin-dir DIR",
		Short: "Update active packages to the latest eligible version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("update accepts a target or --all, not both")
			}
			if !all && len(args) == 0 {
				return fmt.Errorf("update target must be set")
			}
			target := ""
			if len(args) == 1 {
				var err error
				target, err = parseUpdateTarget(args[0])
				if err != nil {
					return err
				}
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			results, err := runtime.Update(cmd.Context(), app.UpdateRequest{
				Target:   target,
				All:      all,
				StoreDir: cfg.StoreDir,
				BinDir:   cfg.BinDir,
				StateDir: cfg.StateDir,
			})
			writeUpdateResults(options, results)
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "update all installed packages")
	cmd.Flags().StringVar(&update.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&update.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}

func writeUpdateResults(options Options, results []app.UpdateInstalledResult) {
	for _, result := range results {
		target := result.Repository + "/" + result.Package
		if result.Reason != "" {
			fmt.Fprintf(options.Out, "%s %s %s %s %s\n", target, result.PreviousVersion, result.CurrentVersion, result.Status, result.Reason)
			continue
		}
		fmt.Fprintf(options.Out, "%s %s %s %s\n", target, result.PreviousVersion, result.CurrentVersion, result.Status)
	}
}
