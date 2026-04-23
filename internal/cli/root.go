package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/config"
	appruntime "github.com/meigma/ghd/internal/runtime"
	"github.com/meigma/ghd/internal/state"
)

// Runtime is the app behavior consumed by the CLI.
type Runtime interface {
	// Download fetches, verifies, and records one release asset.
	Download(ctx context.Context, request app.VerifiedDownloadRequest) (app.VerifiedDownloadResult, error)
	// Install fetches, verifies, extracts, links, and records one package install.
	Install(ctx context.Context, request app.VerifiedInstallRequest) (app.VerifiedInstallResult, error)
	// AddRepository fetches and indexes a repository manifest.
	AddRepository(ctx context.Context, request app.RepositoryAddRequest) (catalog.RepositoryRecord, error)
	// ListRepositories returns indexed repositories.
	ListRepositories(ctx context.Context, indexDir string) ([]catalog.RepositoryRecord, error)
	// RemoveRepository removes a repository from the local index.
	RemoveRepository(ctx context.Context, request app.RepositoryRemoveRequest) error
	// RefreshRepositories refreshes indexed repository manifests.
	RefreshRepositories(ctx context.Context, request app.RepositoryRefreshRequest) ([]catalog.RepositoryRecord, error)
	// ResolvePackage resolves an unqualified package through the local index.
	ResolvePackage(ctx context.Context, request app.ResolvePackageRequest) (app.ResolvePackageResult, error)
	// CheckInstalled reports update availability for installed packages.
	CheckInstalled(ctx context.Context, request app.CheckRequest) ([]app.CheckResult, error)
	// Update upgrades one active installed package when a newer eligible version exists.
	Update(ctx context.Context, request app.UpdateRequest) (app.UpdateResult, error)
	// ListInstalled returns active installed packages.
	ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error)
	// Uninstall removes one active installed package.
	Uninstall(ctx context.Context, request app.UninstallRequest) (state.Record, error)
}

// RuntimeFactory constructs use cases from runtime configuration.
type RuntimeFactory func(context.Context, config.Config) (Runtime, error)

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
		options.RuntimeFactory = func(ctx context.Context, cfg config.Config) (Runtime, error) {
			return appruntime.New(ctx, cfg)
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
	root.PersistentFlags().String("index-dir", "", "local repository index directory")
	root.PersistentFlags().String("state-dir", "", "local installed package state directory")
	root.AddCommand(newDownloadCommand(options))
	root.AddCommand(newInstallCommand(options))
	root.AddCommand(newCheckCommand(options))
	root.AddCommand(newUpdateCommand(options))
	root.AddCommand(newInstalledCommand(options))
	root.AddCommand(newUninstallCommand(options))
	root.AddCommand(newRepositoryCommand(options))
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
