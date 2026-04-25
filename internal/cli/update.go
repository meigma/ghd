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
	var jsonOutput bool
	var allowSignerChange bool
	var update updateOptions
	cmd := &cobra.Command{
		Use:   "update [name|owner/repo/package|--all] --store-dir DIR --bin-dir DIR",
		Short: "Update active packages to the latest eligible version",
		Long:  "Update active packages to the latest eligible version.\n\nOrdinary verified updates can be approved with --yes. If the trusted release signer changes, update requires interactive review or --yes --approve-signer-change --non-interactive.",
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

			mode := detectUpdatePresentationMode(options, jsonOutput)
			var status *statusLine
			var progress app.UpdateProgressFunc
			if mode.statusLine {
				status = newStatusLine(options.Err, mode.color)
				defer status.Clear()
				progress = status.UpdateUpdateProgress
			}

			cfg := config.Load(options.Viper)
			writeTrustedRootNotice(options.Err, cfg.TrustedRootPath)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			results, err := runtime.Update(cmd.Context(), app.UpdateRequest{
				Target:            target,
				All:               all,
				StoreDir:          cfg.StoreDir,
				BinDir:            cfg.BinDir,
				StateDir:          cfg.StateDir,
				TrustRootPath:     cfg.TrustedRootPath,
				AllowSignerChange: allowSignerChange,
				Progress:          progress,
				Approve:           updateApprovalCallback(options, mode, status),
			})
			if status != nil {
				status.Clear()
			}
			if jsonOutput {
				if writeErr := writeUpdateResultsJSON(options, results); writeErr != nil {
					return writeErr
				}
			} else {
				writeUpdateResults(options, results)
				writeUpdateSummary(options.Err, results, mode.enhanced, mode.color, cfg.TrustedRootPath)
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "update all installed packages")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write update results as JSON")
	cmd.Flags().BoolVar(&allowSignerChange, "approve-signer-change", false, "allow update to rotate the trusted release signer; combine with --yes for non-interactive approval")
	cmd.Flags().StringVar(&update.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&update.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}

func writeUpdateResults(options Options, results []app.UpdateInstalledResult) {
	for _, result := range results {
		target := terminalSafeText(result.Repository + "/" + result.Package)
		if result.Reason != "" {
			fmt.Fprintf(options.Out, "%s %s %s %s %s\n", target, terminalSafeText(result.PreviousVersion), terminalSafeText(result.CurrentVersion), result.Status, terminalSafeText(result.Reason))
			continue
		}
		fmt.Fprintf(options.Out, "%s %s %s %s\n", target, terminalSafeText(result.PreviousVersion), terminalSafeText(result.CurrentVersion), result.Status)
	}
}
