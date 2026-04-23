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
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parsePackageVersionTarget("download", args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(outputDir) == "" {
				return fmt.Errorf("--output must be set")
			}

			downloader, err := options.RuntimeFactory(cmd.Context(), config.Load(options.Viper))
			if err != nil {
				return err
			}
			result, err := downloader.Download(cmd.Context(), app.VerifiedDownloadRequest{
				Repository:  target.repository,
				PackageName: target.packageName,
				Version:     target.version,
				OutputDir:   outputDir,
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(options.Err, "verified %s/%s@%s\n", result.Repository, result.PackageName, result.Version)
			fmt.Fprintf(options.Out, "artifact %s\n", result.ArtifactPath)
			fmt.Fprintf(options.Out, "verification %s\n", result.EvidencePath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "directory for the downloaded artifact and verification evidence")
	return cmd
}
