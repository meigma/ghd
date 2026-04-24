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
		Use:   "install package@version|owner/repo/package@version --store-dir DIR --bin-dir DIR",
		Short: "Install and verify one GitHub release package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectInstallPresentationMode(options)
			var status *statusLine
			var progress app.InstallProgressFunc
			if mode.statusLine {
				status = newStatusLine(options.Err, mode.color)
				defer status.Clear()
				progress = status.UpdateInstallProgress
			}

			target, err := parseInstallTarget(args[0])
			if err != nil {
				return err
			}

			cfg := config.Load(options.Viper)
			writeTrustedRootNotice(options.Err, cfg.TrustedRootPath)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if !target.qualified {
				if status != nil {
					status.Update("Refreshing repository index")
				}
				if _, err := runtime.RefreshRepositories(cmd.Context(), app.RepositoryRefreshRequest{
					All:      true,
					IndexDir: cfg.IndexDir,
				}); err != nil {
					return err
				}
				if status != nil {
					status.Update("Resolving package")
				}
				resolved, err := runtime.ResolvePackage(cmd.Context(), app.ResolvePackageRequest{
					PackageName: target.packageName,
					IndexDir:    cfg.IndexDir,
				})
				if err != nil {
					return err
				}
				target.repository = resolved.Repository
				target.packageName = resolved.PackageName
			}
			result, err := runtime.Install(cmd.Context(), app.VerifiedInstallRequest{
				Repository:    target.repository,
				PackageName:   target.packageName,
				Version:       target.version,
				StoreDir:      cfg.StoreDir,
				BinDir:        cfg.BinDir,
				StateDir:      cfg.StateDir,
				TrustRootPath: cfg.TrustedRootPath,
				Progress:      progress,
				Approve:       installApprovalCallback(options, mode, status),
			})
			if err != nil {
				return err
			}

			if status != nil {
				status.Clear()
			}
			writeInstallSummary(options.Err, result, mode.enhanced, mode.color)
			if mode.nonInteractive {
				for _, binary := range result.Binaries {
					fmt.Fprintf(options.Out, "binary %s\n", terminalSafeText(binary.LinkPath))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&install.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&install.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}
