package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
	appruntime "github.com/meigma/ghd/internal/runtime"
)

// DownloadUseCase is the app behavior consumed by the CLI.
type DownloadUseCase interface {
	// Download fetches, verifies, and records one release asset.
	Download(ctx context.Context, request app.VerifiedDownloadRequest) (app.VerifiedDownloadResult, error)
}

// RuntimeFactory constructs use cases from runtime configuration.
type RuntimeFactory func(context.Context, config.Config) (DownloadUseCase, error)

// Options customizes root command construction.
type Options struct {
	// Out receives machine-readable command output.
	Out io.Writer
	// Err receives diagnostics and human-readable status.
	Err io.Writer
	// Viper is the configuration instance used by the command tree.
	Viper *viper.Viper
	// RuntimeFactory wires runtime dependencies for command execution.
	RuntimeFactory RuntimeFactory
}

// NewRootCommand creates the ghd Cobra command tree.
func NewRootCommand(options Options) *cobra.Command {
	if options.Out == nil {
		options.Out = io.Discard
	}
	if options.Err == nil {
		options.Err = io.Discard
	}
	if options.Viper == nil {
		options.Viper = viper.New()
	}
	if options.RuntimeFactory == nil {
		options.RuntimeFactory = func(ctx context.Context, cfg config.Config) (DownloadUseCase, error) {
			return appruntime.NewVerifiedDownloader(ctx, cfg)
		}
	}

	root := &cobra.Command{
		Use:           "ghd",
		Short:         "Securely download GitHub release assets",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initializeConfig(cmd, options.Viper)
		},
	}
	root.SetOut(options.Out)
	root.SetErr(options.Err)
	root.PersistentFlags().String("github-api-url", "", "GitHub REST API base URL")
	root.PersistentFlags().String("trusted-root", "", "Sigstore trusted_root.json path")
	root.AddCommand(newDownloadCommand(options))
	return root
}

func initializeConfig(cmd *cobra.Command, vp *viper.Viper) error {
	vp.SetEnvPrefix("GHD")
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()
	if err := vp.BindPFlags(cmd.Root().PersistentFlags()); err != nil {
		return fmt.Errorf("bind persistent flags: %w", err)
	}
	if err := vp.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("bind flags: %w", err)
	}
	return nil
}
