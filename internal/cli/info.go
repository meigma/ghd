package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

func newInfoCommand(options Options) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "info name|owner/repo|owner/repo/package",
		Short: "Show discovery details for one package",
		Example: strings.TrimSpace(`
ghd info foo --index-dir ./index
ghd info owner/repo
ghd info owner/repo/foo
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectReadOnlyPresentationMode(options, jsonOutput)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}

			target, err := parseInfoTarget(args[0])
			if err != nil {
				return err
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			var result app.PackageInfoResult
			if target.unqualifiedName != "" {
				if status != nil {
					status.Show(fmt.Sprintf("Resolving %s from the local index", terminalSafeText(target.unqualifiedName)))
				}
				resolved, err := runtime.ResolvePackage(cmd.Context(), app.ResolvePackageRequest{
					PackageName: target.unqualifiedName,
					IndexDir:    cfg.IndexDir,
				})
				if err != nil {
					return err
				}
				target.repository = resolved.Repository
				target.packageName = resolved.PackageName
			}
			if status != nil {
				status.Show(fmt.Sprintf("Fetching package details from %s", terminalSafeText(target.repository.String())))
			}
			result, err = runtime.InfoPackage(cmd.Context(), app.PackageInfoRequest{
				Repository:  target.repository,
				PackageName: target.packageName,
				IndexDir:    cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			if status != nil {
				status.Clear()
			}
			if jsonOutput {
				return writePackageInfoJSON(options, result)
			}
			if mode.richOutput {
				writePackageInfoTTY(options.Out, result, mode.color)
				return nil
			}
			writePackageInfo(options, result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write package details as JSON")
	return cmd
}

func writePackageInfo(options Options, result app.PackageInfoResult) {
	fmt.Fprintf(options.Out, "repository %s\n", terminalSafeText(result.Repository.String()))
	fmt.Fprintf(options.Out, "package %s\n", terminalSafeText(result.PackageName))
	fmt.Fprintf(options.Out, "signer-workflow %s\n", terminalSafeText(string(result.SignerWorkflow)))
	fmt.Fprintf(options.Out, "tag-pattern %s\n", terminalSafeText(result.TagPattern))
	fmt.Fprintf(options.Out, "binaries %s\n", strings.Join(terminalSafeStrings(result.Binaries), ","))
	for _, asset := range result.Assets {
		fmt.Fprintf(options.Out, "asset %s/%s %s\n", terminalSafeText(asset.OS), terminalSafeText(asset.Arch), terminalSafeText(asset.Pattern))
	}
}
