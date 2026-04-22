package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

type installOptions struct {
	storeDir string
	binDir   string
}

func newInstallCommand(options Options) *cobra.Command {
	var install installOptions
	cmd := &cobra.Command{
		Use:   "install owner/repo/package@version --store-dir DIR --bin-dir DIR",
		Short: "Install and verify one GitHub release package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parsePackageVersionTarget("install", args[0])
			if err != nil {
				return err
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := runtime.Install(cmd.Context(), app.VerifiedInstallRequest{
				Repository:  target.repository,
				PackageName: target.packageName,
				Version:     target.version,
				StoreDir:    cfg.StoreDir,
				BinDir:      cfg.BinDir,
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(options.Err, "installed %s/%s@%s\n", result.Repository, result.PackageName, result.Version)
			for _, binary := range result.Binaries {
				fmt.Fprintf(options.Out, "binary %s\n", binary.LinkPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&install.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&install.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}
