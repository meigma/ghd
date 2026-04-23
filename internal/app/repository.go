package app

import (
	"context"
	"fmt"
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
	// SaveCatalog writes the catalog index to indexDir.
	SaveCatalog(ctx context.Context, indexDir string, index catalog.Index) error
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

// RepositoryListRequest describes a repository list request.
type RepositoryListRequest struct {
	// IndexDir is the local catalog directory.
	IndexDir string
}

// RepositoryListResult contains indexed repository records.
type RepositoryListResult struct {
	// Repositories are the indexed repositories.
	Repositories []catalog.RepositoryRecord
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

// RepositoryRefreshResult describes refreshed repository records.
type RepositoryRefreshResult struct {
	// Repositories are the refreshed repositories.
	Repositories []catalog.RepositoryRecord
}

// ResolvePackageRequest describes an unqualified package lookup.
type ResolvePackageRequest struct {
	// PackageName is the unqualified package name.
	PackageName string
	// IndexDir is the local catalog directory.
	IndexDir string
}

// ResolvePackageResult contains a concrete package target.
type ResolvePackageResult struct {
	// Repository is the resolved package repository.
	Repository verification.Repository
	// PackageName is the resolved package name.
	PackageName string
}

// NewRepositoryCatalog creates a repository catalog use case.
func NewRepositoryCatalog(deps RepositoryCatalogDependencies) (*RepositoryCatalog, error) {
	if deps.Manifests == nil {
		return nil, fmt.Errorf("manifest source must be set")
	}
	if deps.Store == nil {
		return nil, fmt.Errorf("catalog store must be set")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &RepositoryCatalog{manifests: deps.Manifests, store: deps.Store, now: now}, nil
}

// AddRepository fetches and indexes a repository manifest.
func (c *RepositoryCatalog) AddRepository(ctx context.Context, request RepositoryAddRequest) (catalog.RepositoryRecord, error) {
	if err := validateRepositoryRequest(request.Repository, request.IndexDir); err != nil {
		return catalog.RepositoryRecord{}, err
	}
	record, err := c.fetchRecord(ctx, request.Repository)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	index, err := c.store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	index, err = index.UpsertRepository(record)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	if err := c.store.SaveCatalog(ctx, request.IndexDir, index); err != nil {
		return catalog.RepositoryRecord{}, err
	}
	return record, nil
}

// ListRepositories returns indexed repository records.
func (c *RepositoryCatalog) ListRepositories(ctx context.Context, request RepositoryListRequest) (RepositoryListResult, error) {
	if strings.TrimSpace(request.IndexDir) == "" {
		return RepositoryListResult{}, fmt.Errorf("index directory must be set")
	}
	index, err := c.store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return RepositoryListResult{}, err
	}
	return RepositoryListResult{Repositories: index.Normalize().Repositories}, nil
}

// RemoveRepository removes a repository from the local catalog.
func (c *RepositoryCatalog) RemoveRepository(ctx context.Context, request RepositoryRemoveRequest) error {
	if err := validateRepositoryRequest(request.Repository, request.IndexDir); err != nil {
		return err
	}
	index, err := c.store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return err
	}
	index, removed := index.RemoveRepository(request.Repository)
	if !removed {
		return fmt.Errorf("repository %s is not indexed", request.Repository)
	}
	return c.store.SaveCatalog(ctx, request.IndexDir, index)
}

// RefreshRepositories refreshes one repository or every indexed repository.
func (c *RepositoryCatalog) RefreshRepositories(ctx context.Context, request RepositoryRefreshRequest) (RepositoryRefreshResult, error) {
	if strings.TrimSpace(request.IndexDir) == "" {
		return RepositoryRefreshResult{}, fmt.Errorf("index directory must be set")
	}
	index, err := c.store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return RepositoryRefreshResult{}, err
	}
	if request.Repository.IsZero() && !request.All {
		return RepositoryRefreshResult{}, fmt.Errorf("refresh target must be owner/repo or --all")
	}

	var repositories []verification.Repository
	if !request.Repository.IsZero() {
		if _, ok := index.Repository(request.Repository); !ok {
			return RepositoryRefreshResult{}, fmt.Errorf("repository %s is not indexed", request.Repository)
		}
		repositories = append(repositories, request.Repository)
	} else {
		for _, record := range index.Normalize().Repositories {
			repositories = append(repositories, record.Repository)
		}
	}
	refreshed := make([]catalog.RepositoryRecord, 0, len(repositories))
	next := index
	for _, repository := range repositories {
		record, err := c.fetchRecord(ctx, repository)
		if err != nil {
			return RepositoryRefreshResult{}, err
		}
		next, err = next.UpsertRepository(record)
		if err != nil {
			return RepositoryRefreshResult{}, err
		}
		refreshed = append(refreshed, record)
	}
	if err := c.store.SaveCatalog(ctx, request.IndexDir, next); err != nil {
		return RepositoryRefreshResult{}, err
	}
	return RepositoryRefreshResult{Repositories: refreshed}, nil
}

// ResolvePackage resolves an unqualified package name through the local catalog.
func (c *RepositoryCatalog) ResolvePackage(ctx context.Context, request ResolvePackageRequest) (ResolvePackageResult, error) {
	if strings.TrimSpace(request.IndexDir) == "" {
		return ResolvePackageResult{}, fmt.Errorf("index directory must be set")
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

func (c *RepositoryCatalog) fetchRecord(ctx context.Context, repository verification.Repository) (catalog.RepositoryRecord, error) {
	manifestBytes, err := c.manifests.FetchManifest(ctx, repository)
	if err != nil {
		return catalog.RepositoryRecord{}, fmt.Errorf("fetch ghd.toml: %w", err)
	}
	cfg, err := manifest.Decode(manifestBytes)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	return catalog.NewRepositoryRecord(repository, cfg, c.now())
}

func validateRepositoryRequest(repository verification.Repository, indexDir string) error {
	if strings.TrimSpace(repository.Owner) == "" || strings.TrimSpace(repository.Name) == "" {
		return fmt.Errorf("repository must be owner/repo")
	}
	if strings.Contains(repository.Owner, "/") || strings.Contains(repository.Name, "/") {
		return fmt.Errorf("repository must be owner/repo")
	}
	if strings.TrimSpace(indexDir) == "" {
		return fmt.Errorf("index directory must be set")
	}
	return nil
}
