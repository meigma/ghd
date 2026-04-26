package runtime

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	sigroot "github.com/sigstore/sigstore-go/pkg/root"

	"github.com/meigma/ghd/internal/adapters/archive"
	"github.com/meigma/ghd/internal/adapters/filesystem"
	"github.com/meigma/ghd/internal/adapters/github"
	"github.com/meigma/ghd/internal/adapters/sigstore"
	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

const userAgent = "ghd"

// Runtime contains the application use cases wired to concrete adapters.
type Runtime struct {
	cfg         config.Config
	components  components
	catalog     *app.RepositoryCatalog
	checker     *app.InstalledPackageChecker
	verifier    *app.InstalledPackageVerifier
	updater     *app.PackageUpdater
	installed   *app.InstalledPackages
	uninstaller *app.PackageUninstaller
	doctor      *app.EnvironmentDoctor
	downloader  *app.VerifiedDownloader
	installer   *app.VerifiedInstaller
}

// New wires the application runtime.
func New(ctx context.Context, cfg config.Config) (*Runtime, error) {
	deps, err := newComponents(ctx, cfg)
	if err != nil {
		return nil, err
	}
	repositoryCatalog, err := app.NewRepositoryCatalog(app.RepositoryCatalogDependencies{
		Manifests: deps.githubClient,
		Store:     deps.catalogStore,
	})
	if err != nil {
		return nil, err
	}
	checker, err := app.NewInstalledPackageChecker(app.InstalledPackageCheckerDependencies{
		Manifests:  deps.githubClient,
		Releases:   deps.githubClient,
		StateStore: deps.installedStore,
	})
	if err != nil {
		return nil, err
	}
	installedPackages, err := app.NewInstalledPackages(app.InstalledPackagesDependencies{
		StateStore: deps.installedStore,
	})
	if err != nil {
		return nil, err
	}
	uninstaller, err := app.NewPackageUninstaller(app.PackageUninstallerDependencies{
		StateStore: deps.installedStore,
		FileSystem: filesystem.NewInstaller(),
	})
	if err != nil {
		return nil, err
	}
	doctor, err := app.NewEnvironmentDoctor(app.EnvironmentDoctorDependencies{
		GitHub:      deps.githubClient,
		TrustedRoot: sigstore.NewTrustedRootChecker(),
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{
		cfg:         cfg,
		components:  deps,
		catalog:     repositoryCatalog,
		checker:     checker,
		installed:   installedPackages,
		uninstaller: uninstaller,
		doctor:      doctor,
	}, nil
}

// NewVerifiedDownloader wires the verified download use case.
func NewVerifiedDownloader(ctx context.Context, cfg config.Config) (*app.VerifiedDownloader, error) {
	deps, err := newComponents(ctx, cfg)
	if err != nil {
		return nil, err
	}
	coreVerifier, err := newCoreVerifier(ctx, cfg, deps.githubClient)
	if err != nil {
		return nil, err
	}
	return app.NewVerifiedDownloader(app.VerifiedDownloadDependencies{
		Manifests:      deps.githubClient,
		Assets:         deps.githubClient,
		Downloader:     deps.githubClient,
		Verifier:       coreVerifier,
		EvidenceWriter: deps.evidenceWriter,
		FileSystem:     filesystem.NewInstaller(),
	})
}

// Download fetches, verifies, and records one release asset.
func (r *Runtime) Download(
	ctx context.Context,
	request app.VerifiedDownloadRequest,
) (app.VerifiedDownloadResult, error) {
	if err := r.ensureVerifiedUseCases(ctx); err != nil {
		return app.VerifiedDownloadResult{}, err
	}
	return r.downloader.Download(ctx, request)
}

// Install fetches, verifies, extracts, links, and records one package install.
func (r *Runtime) Install(ctx context.Context, request app.VerifiedInstallRequest) (app.VerifiedInstallResult, error) {
	if err := r.ensureVerifiedUseCases(ctx); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	return r.installer.Install(ctx, request)
}

// AddRepository fetches and indexes a repository manifest.
func (r *Runtime) AddRepository(
	ctx context.Context,
	request app.RepositoryAddRequest,
) (catalog.RepositoryRecord, error) {
	return r.catalog.AddRepository(ctx, request)
}

// ListRepositories returns indexed repositories.
func (r *Runtime) ListRepositories(ctx context.Context, indexDir string) ([]catalog.RepositoryRecord, error) {
	return r.catalog.ListRepositories(ctx, indexDir)
}

// RemoveRepository removes a repository from the local index.
func (r *Runtime) RemoveRepository(ctx context.Context, request app.RepositoryRemoveRequest) error {
	return r.catalog.RemoveRepository(ctx, request)
}

// RefreshRepositories refreshes indexed repository manifests.
func (r *Runtime) RefreshRepositories(
	ctx context.Context,
	request app.RepositoryRefreshRequest,
) ([]catalog.RepositoryRecord, error) {
	return r.catalog.RefreshRepositories(ctx, request)
}

// ResolvePackage resolves an unqualified package through the local index.
func (r *Runtime) ResolvePackage(
	ctx context.Context,
	request app.ResolvePackageRequest,
) (app.ResolvePackageResult, error) {
	return r.catalog.ResolvePackage(ctx, request)
}

// ListPackages returns package-discovery rows.
func (r *Runtime) ListPackages(ctx context.Context, request app.PackageListRequest) ([]app.PackageListResult, error) {
	return r.catalog.ListPackages(ctx, request)
}

// InfoPackage returns one resolved package detail record.
func (r *Runtime) InfoPackage(ctx context.Context, request app.PackageInfoRequest) (app.PackageInfoResult, error) {
	return r.catalog.InfoPackage(ctx, request)
}

// ListInstalled returns active installed packages.
func (r *Runtime) ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error) {
	return r.installed.ListInstalled(ctx, stateDir)
}

// CheckInstalled reports update availability for installed packages.
func (r *Runtime) CheckInstalled(ctx context.Context, request app.CheckRequest) ([]app.CheckResult, error) {
	return r.checker.Check(ctx, request)
}

// VerifyInstalled re-verifies selected active installed packages.
func (r *Runtime) VerifyInstalled(
	ctx context.Context,
	request app.VerifyInstalledRequest,
) ([]app.VerifyInstalledResult, error) {
	if err := r.ensureVerifiedUseCases(ctx); err != nil {
		return nil, err
	}
	return r.verifier.Verify(ctx, request)
}

// Update updates selected active installed packages.
func (r *Runtime) Update(ctx context.Context, request app.UpdateRequest) ([]app.UpdateInstalledResult, error) {
	if err := r.ensureVerifiedUseCases(ctx); err != nil {
		return nil, err
	}
	if request.StoreDir == "" {
		request.StoreDir = r.cfg.StoreDir
	}
	if request.BinDir == "" {
		request.BinDir = r.cfg.BinDir
	}
	return r.updater.Update(ctx, request)
}

// Uninstall removes one active installed package.
func (r *Runtime) Uninstall(ctx context.Context, request app.UninstallRequest) (state.Record, error) {
	if request.BinDir == "" {
		request.BinDir = r.cfg.BinDir
	}
	return r.uninstaller.Uninstall(ctx, request)
}

// Doctor checks local environment readiness.
func (r *Runtime) Doctor(ctx context.Context, request app.DoctorRequest) ([]app.DoctorResult, error) {
	if request.IndexDir == "" {
		request.IndexDir = r.cfg.IndexDir
	}
	if request.StoreDir == "" {
		request.StoreDir = r.cfg.StoreDir
	}
	if request.StateDir == "" {
		request.StateDir = r.cfg.StateDir
	}
	if request.BinDir == "" {
		request.BinDir = r.cfg.BinDir
	}
	if request.TrustedRootPath == "" {
		request.TrustedRootPath = r.cfg.TrustedRootPath
	}
	if request.GitHubToken == "" {
		request.GitHubToken = r.cfg.GitHubToken
	}
	return r.doctor.Doctor(ctx, request)
}

type components struct {
	githubClient   *github.Client
	evidenceWriter filesystem.EvidenceWriter
	catalogStore   filesystem.CatalogStore
	installedStore filesystem.InstalledStore
}

func newComponents(ctx context.Context, cfg config.Config) (components, error) {
	if err := ctx.Err(); err != nil {
		return components{}, err
	}

	githubOptions := []github.Option{github.WithUserAgent(userAgent)}
	if cfg.GitHubBaseURL != "" {
		githubOptions = append(githubOptions, github.WithBaseURL(cfg.GitHubBaseURL))
	}
	if cfg.GitHubToken != "" {
		if !githubBaseURLCanReceiveToken(cfg.GitHubBaseURL) {
			return components{}, fmt.Errorf(
				"refusing to send GitHub token to custom API URL %s; unset GITHUB_TOKEN/GH_TOKEN or use %s",
				cfg.GitHubBaseURL,
				github.DefaultBaseURL,
			)
		}
		githubOptions = append(githubOptions, github.WithToken(cfg.GitHubToken))
	}
	githubClient, err := github.NewClient(githubOptions...)
	if err != nil {
		return components{}, err
	}

	return components{
		githubClient:   githubClient,
		evidenceWriter: filesystem.NewEvidenceWriter(),
		catalogStore:   filesystem.NewCatalogStore(),
		installedStore: filesystem.NewInstalledStore(),
	}, nil
}

func githubBaseURLCanReceiveToken(baseURL string) bool {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return true
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.github.com") {
		return false
	}
	return strings.TrimRight(parsed.Path, "/") == ""
}

func (r *Runtime) ensureVerifiedUseCases(ctx context.Context) error {
	if r.downloader != nil && r.installer != nil && r.updater != nil && r.verifier != nil {
		return nil
	}
	coreVerifier, err := newCoreVerifier(ctx, r.cfg, r.components.githubClient)
	if err != nil {
		return err
	}
	downloader, err := app.NewVerifiedDownloader(app.VerifiedDownloadDependencies{
		Manifests:      r.components.githubClient,
		Assets:         r.components.githubClient,
		Downloader:     r.components.githubClient,
		Verifier:       coreVerifier,
		EvidenceWriter: r.components.evidenceWriter,
		FileSystem:     filesystem.NewInstaller(),
	})
	if err != nil {
		return err
	}
	installer, err := app.NewVerifiedInstaller(app.VerifiedInstallDependencies{
		Manifests:      r.components.githubClient,
		Releases:       r.components.githubClient,
		Assets:         r.components.githubClient,
		Downloader:     r.components.githubClient,
		Verifier:       coreVerifier,
		EvidenceWriter: r.components.evidenceWriter,
		Materializer:   archive.NewMaterializer(),
		FileSystem:     filesystem.NewInstaller(),
		StateStore:     r.components.installedStore,
	})
	if err != nil {
		return err
	}
	updater, err := app.NewPackageUpdater(app.PackageUpdaterDependencies{
		Manifests:      r.components.githubClient,
		Releases:       r.components.githubClient,
		Assets:         r.components.githubClient,
		Downloader:     r.components.githubClient,
		Verifier:       coreVerifier,
		EvidenceWriter: r.components.evidenceWriter,
		EvidenceStore:  r.components.evidenceWriter,
		Materializer:   archive.NewMaterializer(),
		FileSystem:     filesystem.NewInstaller(),
		StateStore:     r.components.installedStore,
	})
	if err != nil {
		return err
	}
	installedVerifier, err := app.NewInstalledPackageVerifier(app.InstalledPackageVerifierDependencies{
		StateStore:    r.components.installedStore,
		Verifier:      coreVerifier,
		EvidenceStore: r.components.evidenceWriter,
		Materializer:  archive.NewMaterializer(),
		FileSystem:    filesystem.NewInstaller(),
	})
	if err != nil {
		return err
	}
	r.downloader = downloader
	r.installer = installer
	r.updater = updater
	r.verifier = installedVerifier
	return nil
}

func newCoreVerifier(
	ctx context.Context,
	cfg config.Config,
	githubClient *github.Client,
) (*verification.Verifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sigstoreOptions := []sigstore.Option{}
	if cfg.TrustedRootPath != "" {
		sigstoreOptions = append(sigstoreOptions, sigstore.WithTrustedRootPath(cfg.TrustedRootPath))
	} else {
		trustedRoot, err := sigroot.FetchTrustedRoot()
		if err != nil {
			return nil, fmt.Errorf("fetch Sigstore trusted root: %w", err)
		}
		githubTrustedRoot, err := sigstore.FetchGitHubTrustedRoot()
		if err != nil {
			return nil, err
		}
		sigstoreOptions = append(sigstoreOptions,
			sigstore.WithPublicGoodTrustedMaterial(trustedRoot),
			sigstore.WithGitHubTrustedMaterial(githubTrustedRoot),
		)
	}
	bundleVerifier, err := sigstore.NewVerifier(sigstoreOptions...)
	if err != nil {
		return nil, err
	}
	return verification.NewVerifier(verification.Dependencies{
		ReleaseResolver:   githubClient,
		AttestationSource: githubClient,
		BundleVerifier:    bundleVerifier,
	})
}
