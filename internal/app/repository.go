package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

// CatalogStore persists the local repository catalog.
type CatalogStore interface {
	// LoadCatalog reads the catalog index from indexDir.
	LoadCatalog(ctx context.Context, indexDir string) (catalog.Index, error)
	// UpsertRepository adds or replaces one indexed repository.
	UpsertRepository(ctx context.Context, indexDir string, record catalog.RepositoryRecord) (catalog.Index, error)
	// UpsertRepositories adds or replaces indexed repositories in one update.
	UpsertRepositories(ctx context.Context, indexDir string, records []catalog.RepositoryRecord) (catalog.Index, error)
	// RemoveRepository removes one indexed repository.
	RemoveRepository(ctx context.Context, indexDir string, repository verification.Repository) (catalog.Index, error)
}

// RepositoryCatalogDependencies contains the ports needed by RepositoryCatalog.
type RepositoryCatalogDependencies struct {
	// Manifests fetches repository manifest bytes.
	Manifests ManifestSource
	// Store persists the local repository catalog.
	Store CatalogStore
	// Now returns the current time for refreshed records.
	Now func() time.Time
}

// RepositoryCatalog implements repository index management.
type RepositoryCatalog struct {
	manifests ManifestSource
	store     CatalogStore
	now       func() time.Time
}

// RepositoryAddRequest describes one repository to add to the local catalog.
type RepositoryAddRequest struct {
	// Repository is the GitHub repository to index.
	Repository verification.Repository
	// IndexDir is the local catalog directory.
	IndexDir string
}

// RepositoryRemoveRequest describes one repository to remove from the catalog.
type RepositoryRemoveRequest struct {
	// Repository is the GitHub repository to remove.
	Repository verification.Repository
	// IndexDir is the local catalog directory.
	IndexDir string
}

// RepositoryRefreshRequest describes repository refresh behavior.
type RepositoryRefreshRequest struct {
	// Repository optionally limits refresh to one repository.
	Repository verification.Repository
	// All refreshes every indexed repository.
	All bool
	// IndexDir is the local catalog directory.
	IndexDir string
}

// ResolvePackageRequest describes an unqualified package lookup.
type ResolvePackageRequest struct {
	// PackageName is the unqualified package name.
	PackageName manifest.PackageName
	// IndexDir is the local catalog directory.
	IndexDir string
}

// ResolvePackageResult contains a concrete package target.
type ResolvePackageResult struct {
	// Repository is the resolved package repository.
	Repository verification.Repository
	// PackageName is the resolved package name.
	PackageName manifest.PackageName
}

// PackageListRequest describes package-discovery list behavior.
type PackageListRequest struct {
	// Repository optionally limits listing to one live repository manifest.
	Repository verification.Repository
	// IndexDir is the local catalog directory for index-backed listing.
	IndexDir string
}

// PackageListResult is one listed package.
type PackageListResult struct {
	// Repository is the package's GitHub repository.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// Binaries are the exposed command names for the package.
	Binaries []string
}

// PackageInfoRequest describes package detail lookup behavior.
type PackageInfoRequest struct {
	// Repository optionally identifies the repository that owns the package.
	Repository verification.Repository
	// PackageName optionally identifies one package within Repository.
	PackageName manifest.PackageName
	// UnqualifiedName optionally resolves one package through the local index.
	UnqualifiedName string
	// IndexDir is the local catalog directory for unqualified resolution.
	IndexDir string
}

// PackageInfoAsset is one declared package asset.
type PackageInfoAsset struct {
	// OS is the Go-style target operating system.
	OS string
	// Arch is the Go-style target architecture.
	Arch string
	// Pattern is the declared asset naming pattern.
	Pattern string
}

// PackageInfoResult is one resolved package detail record.
type PackageInfoResult struct {
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// SignerWorkflow is the repository's trusted signer workflow identity.
	SignerWorkflow verification.WorkflowIdentity
	// TagPattern is the effective release tag pattern for the package.
	TagPattern string
	// Binaries are the exposed command names for the package.
	Binaries []string
	// Assets are the declared package assets.
	Assets []PackageInfoAsset
}

// NewRepositoryCatalog creates a repository catalog use case.
func NewRepositoryCatalog(deps RepositoryCatalogDependencies) (*RepositoryCatalog, error) {
	if deps.Manifests == nil {
		return nil, errors.New("manifest source must be set")
	}
	if deps.Store == nil {
		return nil, errors.New("catalog store must be set")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &RepositoryCatalog{manifests: deps.Manifests, store: deps.Store, now: now}, nil
}

// AddRepository fetches and indexes a repository manifest.
func (c *RepositoryCatalog) AddRepository(
	ctx context.Context,
	request RepositoryAddRequest,
) (catalog.RepositoryRecord, error) {
	if err := validateRepositoryRequest(request.Repository, request.IndexDir); err != nil {
		return catalog.RepositoryRecord{}, err
	}
	record, err := c.fetchRecord(ctx, request.Repository)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	if _, err := c.store.UpsertRepository(ctx, request.IndexDir, record); err != nil {
		return catalog.RepositoryRecord{}, err
	}
	return record, nil
}

// ListRepositories returns indexed repository records.
func (c *RepositoryCatalog) ListRepositories(ctx context.Context, indexDir string) ([]catalog.RepositoryRecord, error) {
	if strings.TrimSpace(indexDir) == "" {
		return nil, errors.New("index directory must be set")
	}
	index, err := c.store.LoadCatalog(ctx, indexDir)
	if err != nil {
		return nil, err
	}
	return index.Normalize().Repositories, nil
}

// RemoveRepository removes a repository from the local catalog.
func (c *RepositoryCatalog) RemoveRepository(ctx context.Context, request RepositoryRemoveRequest) error {
	if err := validateRepositoryRequest(request.Repository, request.IndexDir); err != nil {
		return err
	}
	_, err := c.store.RemoveRepository(ctx, request.IndexDir, request.Repository)
	return err
}

// RefreshRepositories refreshes one repository or every indexed repository.
func (c *RepositoryCatalog) RefreshRepositories(
	ctx context.Context,
	request RepositoryRefreshRequest,
) ([]catalog.RepositoryRecord, error) {
	if strings.TrimSpace(request.IndexDir) == "" {
		return nil, errors.New("index directory must be set")
	}
	index, err := c.store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return nil, err
	}
	if request.Repository.IsZero() && !request.All {
		return nil, errors.New("refresh target must be owner/repo or --all")
	}

	var repositories []verification.Repository
	if !request.Repository.IsZero() {
		if _, ok := index.Repository(request.Repository); !ok {
			return nil, fmt.Errorf("repository %s is not indexed", request.Repository)
		}
		repositories = append(repositories, request.Repository)
	} else {
		for _, record := range index.Normalize().Repositories {
			repositories = append(repositories, record.Repository)
		}
	}
	refreshed := make([]catalog.RepositoryRecord, 0, len(repositories))
	for _, repository := range repositories {
		record, err := c.fetchRecord(ctx, repository)
		if err != nil {
			return nil, err
		}
		refreshed = append(refreshed, record)
	}
	if _, err := c.store.UpsertRepositories(ctx, request.IndexDir, refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

// ResolvePackage resolves an unqualified package name through the local catalog.
func (c *RepositoryCatalog) ResolvePackage(
	ctx context.Context,
	request ResolvePackageRequest,
) (ResolvePackageResult, error) {
	if strings.TrimSpace(request.IndexDir) == "" {
		return ResolvePackageResult{}, errors.New("index directory must be set")
	}
	index, err := c.store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return ResolvePackageResult{}, err
	}
	resolved, err := index.ResolvePackage(request.PackageName)
	if err != nil {
		return ResolvePackageResult{}, err
	}
	return ResolvePackageResult{Repository: resolved.Repository, PackageName: resolved.PackageName}, nil
}

// ListPackages returns package-discovery rows from the local index or one live repository.
func (c *RepositoryCatalog) ListPackages(ctx context.Context, request PackageListRequest) ([]PackageListResult, error) {
	if request.Repository.IsZero() {
		if strings.TrimSpace(request.IndexDir) == "" {
			return nil, errors.New("index directory must be set")
		}
		index, err := c.store.LoadCatalog(ctx, request.IndexDir)
		if err != nil {
			return nil, err
		}
		return packageListResultsFromIndex(index.Normalize()), nil
	}

	record, err := c.fetchRecord(ctx, request.Repository)
	if err != nil {
		return nil, err
	}
	return packageListResultsFromRecord(record), nil
}

// InfoPackage returns one package's detailed discovery information.
func (c *RepositoryCatalog) InfoPackage(ctx context.Context, request PackageInfoRequest) (PackageInfoResult, error) {
	repository := request.Repository
	packageName := request.PackageName
	if strings.TrimSpace(request.UnqualifiedName) != "" {
		unqualifiedName, err := manifest.NewPackageName(request.UnqualifiedName)
		if err != nil {
			return PackageInfoResult{}, err
		}
		resolved, err := c.ResolvePackage(ctx, ResolvePackageRequest{
			PackageName: unqualifiedName,
			IndexDir:    request.IndexDir,
		})
		if err != nil {
			return PackageInfoResult{}, err
		}
		repository = resolved.Repository
		packageName = resolved.PackageName
	}
	if repository.IsZero() {
		return PackageInfoResult{}, errors.New("info target must identify a repository")
	}
	if !packageName.IsZero() {
		if err := packageName.Validate(); err != nil {
			return PackageInfoResult{}, err
		}
	}

	cfg, err := c.fetchConfig(ctx, repository)
	if err != nil {
		return PackageInfoResult{}, err
	}
	if packageName.IsZero() {
		if len(cfg.Packages) != 1 {
			return PackageInfoResult{}, fmt.Errorf(
				"repository %s declares multiple packages; use %s/package",
				repository,
				repository,
			)
		}
		packageName = cfg.Packages[0].Name
	}
	return c.packageInfoResult(repository, cfg, packageName)
}

func (c *RepositoryCatalog) fetchConfig(
	ctx context.Context,
	repository verification.Repository,
) (manifest.Config, error) {
	manifestBytes, err := c.manifests.FetchManifest(ctx, repository)
	if err != nil {
		return manifest.Config{}, fmt.Errorf("fetch ghd.toml: %w", err)
	}
	return manifest.Decode(manifestBytes)
}

func (c *RepositoryCatalog) fetchRecord(
	ctx context.Context,
	repository verification.Repository,
) (catalog.RepositoryRecord, error) {
	cfg, err := c.fetchConfig(ctx, repository)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	return catalog.NewRepositoryRecord(repository, cfg, c.now())
}

func (c *RepositoryCatalog) packageInfoResult(
	repository verification.Repository,
	cfg manifest.Config,
	packageName manifest.PackageName,
) (PackageInfoResult, error) {
	pkg, err := cfg.Package(packageName)
	if err != nil {
		return PackageInfoResult{}, err
	}
	record, err := catalog.NewRepositoryRecord(repository, cfg, c.now())
	if err != nil {
		return PackageInfoResult{}, err
	}

	var binaries []string
	for _, summary := range record.Packages {
		if summary.Name == packageName.String() {
			binaries = append([]string(nil), summary.Binaries...)
			break
		}
	}
	if len(binaries) == 0 {
		return PackageInfoResult{}, fmt.Errorf(
			"repository %s package %q has no exposed binaries",
			repository,
			packageName,
		)
	}

	assets := make([]PackageInfoAsset, 0, len(pkg.Assets))
	for _, asset := range pkg.Assets {
		assets = append(assets, PackageInfoAsset{
			OS:      asset.OS,
			Arch:    asset.Arch,
			Pattern: asset.Pattern,
		})
	}
	sort.Slice(assets, func(i, j int) bool {
		left := strings.ToLower(assets[i].OS + "/" + assets[i].Arch + "/" + assets[i].Pattern)
		right := strings.ToLower(assets[j].OS + "/" + assets[j].Arch + "/" + assets[j].Pattern)
		return left < right
	})

	return PackageInfoResult{
		Repository:     repository,
		PackageName:    packageName,
		SignerWorkflow: cfg.Provenance.TrustedSignerWorkflow(),
		TagPattern:     pkg.EffectiveTagPattern(),
		Binaries:       binaries,
		Assets:         assets,
	}, nil
}

func packageListResultsFromIndex(index catalog.Index) []PackageListResult {
	results := make([]PackageListResult, 0, len(index.Repositories))
	for _, record := range index.Repositories {
		results = append(results, packageListResultsFromRecord(record)...)
	}
	return results
}

func packageListResultsFromRecord(record catalog.RepositoryRecord) []PackageListResult {
	results := make([]PackageListResult, 0, len(record.Packages))
	for _, pkg := range record.Packages {
		results = append(results, PackageListResult{
			Repository:  record.Repository,
			PackageName: manifest.PackageName(pkg.Name),
			Binaries:    append([]string(nil), pkg.Binaries...),
		})
	}
	return results
}

func validateRepositoryRequest(repository verification.Repository, indexDir string) error {
	if err := repository.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(indexDir) == "" {
		return errors.New("index directory must be set")
	}
	return nil
}
