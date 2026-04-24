package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/verification"
)

func newListCommand(options Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [owner/repo]",
		Short: "List available packages",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var repository verification.Repository
			if len(args) == 1 {
				var err error
				repository, err = parseListTarget(args[0])
				if err != nil {
					return err
				}
			}

			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			results, err := runtime.ListPackages(cmd.Context(), app.PackageListRequest{
				Repository: repository,
				IndexDir:   cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			writePackageList(options, results)
			return nil
		},
	}
	return cmd
}

func writePackageList(options Options, results []app.PackageListResult) {
	for _, result := range results {
		target := result.Repository.String() + "/" + result.PackageName
		if len(result.Binaries) == 0 {
			fmt.Fprintln(options.Out, target)
			continue
		}
		fmt.Fprintf(options.Out, "%s %s\n", target, strings.Join(result.Binaries, ","))
	}
}
