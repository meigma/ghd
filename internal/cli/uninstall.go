package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

type uninstallOptions struct {
	storeDir string
	binDir   string
}

func newUninstallCommand(options Options) *cobra.Command {
	var uninstall uninstallOptions
	cmd := &cobra.Command{
		Use:   "uninstall name|owner/repo/package",
		Short: "Uninstall one active package",
		Example: strings.TrimSpace(`
ghd uninstall package --state-dir ./state --store-dir ./store --bin-dir ./bin
ghd --non-interactive uninstall owner/repo/package --state-dir ./state --store-dir ./store --bin-dir ./bin
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectUninstallPresentationMode(options)
			var status *statusLine
			var progress app.UninstallProgressFunc
			if mode.statusLine {
				status = newStatusLine(options.Err, mode.color)
				defer status.Clear()
				progress = status.UpdateUninstallProgress
			}

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
				BinDir:   cfg.BinDir,
				StateDir: cfg.StateDir,
				Progress: progress,
			})
			if err != nil {
				return err
			}
			if status != nil {
				status.Clear()
			}
			writeUninstallSummary(options.Err, result, mode.enhanced, mode.color)
			return nil
		},
	}
	cmd.Flags().StringVar(&uninstall.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&uninstall.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}
