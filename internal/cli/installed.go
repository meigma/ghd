package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/state"
)

func newInstalledCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "installed",
		Short: "List installed packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := runtime.ListInstalled(cmd.Context(), app.InstalledListRequest{
				StateDir: cfg.StateDir,
			})
			if err != nil {
				return err
			}
			writeInstalledList(options, result.Records)
			return nil
		},
	}
}

func writeInstalledList(options Options, records []state.Record) {
	for _, record := range records {
		binaries := make([]string, 0, len(record.Binaries))
		for _, binary := range record.Binaries {
			binaries = append(binaries, binary.Name)
		}
		fmt.Fprintf(options.Out, "%s/%s %s %s\n", record.Repository, record.Package, record.Version, strings.Join(binaries, ","))
	}
}
