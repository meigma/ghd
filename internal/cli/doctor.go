package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

type doctorOptions struct {
	storeDir string
	binDir   string
}

func newDoctorCommand(options Options) *cobra.Command {
	var doctor doctorOptions
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local environment readiness",
		Example: strings.TrimSpace(`
ghd doctor --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin
ghd doctor --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin --json
ghd --non-interactive doctor --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := detectReadOnlyPresentationMode(options, jsonOutput)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if status != nil {
				status.Show("Checking local environment readiness")
			}
			results, err := runtime.Doctor(cmd.Context(), app.DoctorRequest{
				GitHubToken:     cfg.GitHubToken,
				TrustedRootPath: cfg.TrustedRootPath,
				IndexDir:        cfg.IndexDir,
				StoreDir:        cfg.StoreDir,
				StateDir:        cfg.StateDir,
				BinDir:          cfg.BinDir,
				PathEnv:         os.Getenv("PATH"),
			})
			if status != nil {
				status.Clear()
			}
			if jsonOutput {
				if writeErr := writeDoctorResultsJSON(options, results); writeErr != nil {
					return writeErr
				}
			} else {
				if mode.richOutput {
					writeDoctorResultsTTY(options.Out, results, mode.color)
				} else {
					writeDoctorResults(options, results)
				}
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write doctor checks as JSON")
	cmd.Flags().StringVar(&doctor.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&doctor.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}

func writeDoctorResults(options Options, results []app.DoctorResult) {
	for _, result := range results {
		fmt.Fprintf(
			options.Out,
			"%s %s %s\n",
			result.Status,
			terminalSafeText(result.ID),
			terminalSafeText(result.Message),
		)
	}
}
