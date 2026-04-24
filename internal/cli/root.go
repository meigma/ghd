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
	// ListPackages returns package-discovery rows.
	ListPackages(ctx context.Context, request app.PackageListRequest) ([]app.PackageListResult, error)
	// InfoPackage returns one resolved package detail record.
	InfoPackage(ctx context.Context, request app.PackageInfoRequest) (app.PackageInfoResult, error)
	// CheckInstalled reports update availability for installed packages.
	CheckInstalled(ctx context.Context, request app.CheckRequest) ([]app.CheckResult, error)
	// VerifyInstalled re-verifies selected active installed packages.
	VerifyInstalled(ctx context.Context, request app.VerifyInstalledRequest) ([]app.VerifyInstalledResult, error)
	// Update updates selected active installed packages.
	Update(ctx context.Context, request app.UpdateRequest) ([]app.UpdateInstalledResult, error)
	// ListInstalled returns active installed packages.
	ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error)
	// Uninstall removes one active installed package.
	Uninstall(ctx context.Context, request app.UninstallRequest) (state.Record, error)
	// Doctor checks local environment readiness.
	Doctor(ctx context.Context, request app.DoctorRequest) ([]app.DoctorResult, error)
}

// RuntimeFactory constructs use cases from runtime configuration.
type RuntimeFactory func(context.Context, config.Config) (Runtime, error)

// InstallConfirmationFunc confirms whether a verified artifact should be installed.
type InstallConfirmationFunc func(context.Context, app.InstallApproval) error

// UpdateConfirmationFunc confirms whether a verified artifact should replace an installed package.
type UpdateConfirmationFunc func(context.Context, app.UpdateApproval) error

// Options customizes root command construction.
type Options struct {
	// In receives interactive command input.
	In io.Reader
	// Out receives machine-readable command output.
	Out io.Writer
	// Err receives diagnostics and human-readable status.
	Err io.Writer
	// Viper is the configuration instance used by the command tree.
	Viper *viper.Viper
	// RuntimeFactory wires runtime dependencies for command execution.
	RuntimeFactory RuntimeFactory
	// InstallConfirmation overrides the default interactive install confirmation prompt.
	InstallConfirmation InstallConfirmationFunc
	// UpdateConfirmation overrides the default interactive update confirmation prompt.
	UpdateConfirmation UpdateConfirmationFunc
	// StdinTTY overrides terminal detection for stdin in tests.
	StdinTTY *bool
	// StdoutTTY overrides terminal detection for stdout in tests.
	StdoutTTY *bool
	// StderrTTY overrides terminal detection for stderr in tests.
	StderrTTY *bool
	// ColorEnabled overrides color detection in tests.
	ColorEnabled *bool
}

// NewRootCommand creates the ghd Cobra command tree.
func NewRootCommand(options Options) *cobra.Command {
	if options.In == nil {
		options.In = strings.NewReader("")
	}
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
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)
	root.PersistentFlags().String("github-api-url", "", "GitHub REST API base URL")
	root.PersistentFlags().String("trusted-root", "", "Sigstore trusted_root.json path")
	root.PersistentFlags().String("index-dir", "", "local repository index directory")
	root.PersistentFlags().String("state-dir", "", "local installed package state directory")
	root.PersistentFlags().Bool("non-interactive", false, "disable prompts, colors, and transient terminal UI")
	root.PersistentFlags().Bool("yes", false, "approve verified install and update actions without prompting")
	root.AddCommand(newDownloadCommand(options))
	root.AddCommand(newInstallCommand(options))
	root.AddCommand(newListCommand(options))
	root.AddCommand(newInfoCommand(options))
	root.AddCommand(newCheckCommand(options))
	root.AddCommand(newVerifyCommand(options))
	root.AddCommand(newUpdateCommand(options))
	root.AddCommand(newInstalledCommand(options))
	root.AddCommand(newUninstallCommand(options))
	root.AddCommand(newDoctorCommand(options))
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
