package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

func newDownloadCommand(options Options) *cobra.Command {
	var outputDir string
	cmd := &cobra.Command{
		Use:   "download owner/repo/package@version --output DIR",
		Short: "Download and verify one GitHub release asset",
		Example: strings.TrimSpace(`
ghd download owner/repo/package@version --output ./out
ghd --non-interactive download owner/repo/package@version --output ./out
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectDownloadPresentationMode(options)
			var status *statusLine
			var progress app.VerifiedDownloadProgressFunc
			if mode.statusLine {
				status = newStatusLine(options.Err, mode.color)
				defer status.Clear()
				progress = status.UpdateDownloadProgress
			}

			target, err := parsePackageVersionTarget("download", args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(outputDir) == "" {
				return fmt.Errorf("--output must be set")
			}

			cfg := config.Load(options.Viper)
			writeTrustedRootNotice(options.Err, cfg.TrustedRootPath)
			downloader, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := downloader.Download(cmd.Context(), app.VerifiedDownloadRequest{
				Repository:  target.repository,
				PackageName: target.packageName,
				Version:     target.version,
				OutputDir:   outputDir,
				Progress:    progress,
			})
			if err != nil {
				return err
			}

			if status != nil {
				status.Clear()
			}
			writeDownloadSummary(options.Err, result, mode.enhanced, mode.color, cfg.TrustedRootPath)
			if !mode.enhanced {
				fmt.Fprintf(options.Out, "artifact %s\n", terminalSafeText(result.ArtifactPath))
				fmt.Fprintf(options.Out, "verification %s\n", terminalSafeText(result.EvidencePath))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "directory for the downloaded artifact and verification evidence")
	return cmd
}
