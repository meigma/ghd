package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

func newInfoCommand(options Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info name|owner/repo|owner/repo/package",
		Short: "Show one package's discovery details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseInfoTarget(args[0])
			if err != nil {
				return err
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := runtime.InfoPackage(cmd.Context(), app.PackageInfoRequest{
				Repository:      target.repository,
				PackageName:     target.packageName,
				UnqualifiedName: target.unqualifiedName,
				IndexDir:        cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			writePackageInfo(options, result)
			return nil
		},
	}
	return cmd
}

func writePackageInfo(options Options, result app.PackageInfoResult) {
	fmt.Fprintf(options.Out, "repository %s\n", result.Repository)
	fmt.Fprintf(options.Out, "package %s\n", result.PackageName)
	fmt.Fprintf(options.Out, "signer-workflow %s\n", result.SignerWorkflow)
	fmt.Fprintf(options.Out, "tag-pattern %s\n", result.TagPattern)
	fmt.Fprintf(options.Out, "binaries %s\n", strings.Join(result.Binaries, ","))
	for _, asset := range result.Assets {
		fmt.Fprintf(options.Out, "asset %s/%s %s\n", asset.OS, asset.Arch, asset.Pattern)
	}
}
