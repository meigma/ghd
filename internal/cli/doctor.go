package cli

import (
	"fmt"
	"os"

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
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local environment readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
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
			writeDoctorResults(options, results)
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&doctor.storeDir, "store-dir", "", "managed store directory")
	cmd.Flags().StringVar(&doctor.binDir, "bin-dir", "", "managed binary link directory")
	return cmd
}

func writeDoctorResults(options Options, results []app.DoctorResult) {
	for _, result := range results {
		fmt.Fprintf(options.Out, "%s %s %s\n", result.Status, result.ID, result.Message)
	}
}
