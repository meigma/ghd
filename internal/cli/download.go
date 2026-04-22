package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/verification"
)

type downloadOptions struct {
	outputDir string
}

func newDownloadCommand(options Options) *cobra.Command {
	var download downloadOptions
	cmd := &cobra.Command{
		Use:   "download owner/repo/package@version --output DIR",
		Short: "Download and verify one GitHub release asset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseDownloadTarget(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(download.outputDir) == "" {
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
				OutputDir:   download.outputDir,
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
	cmd.Flags().StringVarP(&download.outputDir, "output", "o", "", "directory for the downloaded artifact and verification evidence")
	return cmd
}

type downloadTarget struct {
	repository  verification.Repository
	packageName string
	version     string
}

func parseDownloadTarget(value string) (downloadTarget, error) {
	value = strings.TrimSpace(value)
	targetPart, version, found := strings.Cut(value, "@")
	if !found || strings.TrimSpace(version) == "" {
		return downloadTarget{}, fmt.Errorf("download target must be owner/repo/package@version")
	}
	parts := strings.Split(targetPart, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return downloadTarget{}, fmt.Errorf("download target must be owner/repo/package@version")
	}
	if strings.Contains(version, "/") {
		return downloadTarget{}, fmt.Errorf("download target must be owner/repo/package@version")
	}
	return downloadTarget{
		repository: verification.Repository{
			Owner: parts[0],
			Name:  parts[1],
		},
		packageName: parts[2],
		version:     version,
	}, nil
}
