package runtime

import (
	"context"
	"fmt"

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
	installed   *app.InstalledPackages
	uninstaller *app.PackageUninstaller
	downloader  *app.VerifiedDownloader
	installer   *app.VerifiedInstaller
}

// New wires the application runtime.
func New(ctx context.Context, cfg config.Config) (*Runtime, error) {
	components, err := newComponents(ctx, cfg)
	if err != nil {
		return nil, err
	}
	repositoryCatalog, err := app.NewRepositoryCatalog(app.RepositoryCatalogDependencies{
		Manifests: components.githubClient,
		Store:     components.catalogStore,
	})
	if err != nil {
		return nil, err
	}
	checker, err := app.NewInstalledPackageChecker(app.InstalledPackageCheckerDependencies{
		Manifests:  components.githubClient,
		Releases:   components.githubClient,
		StateStore: components.installedStore,
	})
	if err != nil {
		return nil, err
	}
	installedPackages, err := app.NewInstalledPackages(app.InstalledPackagesDependencies{
		StateStore: components.installedStore,
	})
	if err != nil {
		return nil, err
	}
	uninstaller, err := app.NewPackageUninstaller(app.PackageUninstallerDependencies{
		StateStore: components.installedStore,
		FileSystem: filesystem.NewInstaller(),
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{
		cfg:         cfg,
		components:  components,
		catalog:     repositoryCatalog,
		checker:     checker,
		installed:   installedPackages,
		uninstaller: uninstaller,
	}, nil
}

// NewVerifiedDownloader wires the verified download use case.
func NewVerifiedDownloader(ctx context.Context, cfg config.Config) (*app.VerifiedDownloader, error) {
	components, err := newComponents(ctx, cfg)
	if err != nil {
		return nil, err
	}
	coreVerifier, err := newCoreVerifier(ctx, cfg, components.githubClient)
	if err != nil {
		return nil, err
	}
	return app.NewVerifiedDownloader(app.VerifiedDownloadDependencies{
		Manifests:      components.githubClient,
		Assets:         components.githubClient,
		Downloader:     components.githubClient,
		Verifier:       coreVerifier,
		EvidenceWriter: components.evidenceWriter,
	})
}

// Download fetches, verifies, and records one release asset.
func (r *Runtime) Download(ctx context.Context, request app.VerifiedDownloadRequest) (app.VerifiedDownloadResult, error) {
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
func (r *Runtime) AddRepository(ctx context.Context, request app.RepositoryAddRequest) (catalog.RepositoryRecord, error) {
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
func (r *Runtime) RefreshRepositories(ctx context.Context, request app.RepositoryRefreshRequest) ([]catalog.RepositoryRecord, error) {
	return r.catalog.RefreshRepositories(ctx, request)
}

// ResolvePackage resolves an unqualified package through the local index.
func (r *Runtime) ResolvePackage(ctx context.Context, request app.ResolvePackageRequest) (app.ResolvePackageResult, error) {
	return r.catalog.ResolvePackage(ctx, request)
}

// ListInstalled returns active installed packages.
func (r *Runtime) ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error) {
	return r.installed.ListInstalled(ctx, stateDir)
}

// CheckInstalled reports update availability for installed packages.
func (r *Runtime) CheckInstalled(ctx context.Context, request app.CheckRequest) ([]app.CheckResult, error) {
	return r.checker.Check(ctx, request)
}

// Uninstall removes one active installed package.
func (r *Runtime) Uninstall(ctx context.Context, request app.UninstallRequest) (state.Record, error) {
	if request.BinDir == "" {
		request.BinDir = r.cfg.BinDir
	}
	return r.uninstaller.Uninstall(ctx, request)
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

func (r *Runtime) ensureVerifiedUseCases(ctx context.Context) error {
	if r.downloader != nil && r.installer != nil {
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
	})
	if err != nil {
		return err
	}
	installer, err := app.NewVerifiedInstaller(app.VerifiedInstallDependencies{
		Manifests:      r.components.githubClient,
		Assets:         r.components.githubClient,
		Downloader:     r.components.githubClient,
		Verifier:       coreVerifier,
		EvidenceWriter: r.components.evidenceWriter,
		Archives:       archive.NewTarGzipExtractor(),
		FileSystem:     filesystem.NewInstaller(),
		StateStore:     r.components.installedStore,
	})
	if err != nil {
		return err
	}
	r.downloader = downloader
	r.installer = installer
	return nil
}

func newCoreVerifier(ctx context.Context, cfg config.Config, githubClient *github.Client) (*verification.Verifier, error) {
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
