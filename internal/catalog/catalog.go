package catalog

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

const schemaVersion = 1

// Index is the persisted local repository catalog.
type Index struct {
	// SchemaVersion is the index file schema version.
	SchemaVersion int `json:"schema_version"`
	// Repositories are the indexed GitHub repositories.
	Repositories []RepositoryRecord `json:"repositories"`
}

// RepositoryRecord is one cached repository manifest summary.
type RepositoryRecord struct {
	// Repository is the GitHub owner/repository name.
	Repository verification.Repository `json:"repository"`
	// Packages are the installable packages from the repository manifest.
	Packages []PackageSummary `json:"packages"`
	// RefreshedAt is when the repository manifest was last fetched.
	RefreshedAt time.Time `json:"refreshed_at"`
}

// PackageSummary is the package data needed for index listing and resolution.
type PackageSummary struct {
	// Name is the package name within the repository.
	Name string `json:"name"`
	// Description is the human-readable package description.
	Description string `json:"description,omitempty"`
	// Binaries are the exposed binary command names for this package.
	Binaries []string `json:"binaries,omitempty"`
}

// ResolvedPackage is a package name resolved through the local index.
type ResolvedPackage struct {
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName string
}

// AmbiguousPackageError reports an unqualified package name with multiple matches.
type AmbiguousPackageError struct {
	// PackageName is the unqualified package name.
	PackageName string
	// Matches are packages that expose PackageName as a package or binary name.
	Matches []ResolvedPackage
}

// Error describes the ambiguous package lookup.
func (e AmbiguousPackageError) Error() string {
	matches := make([]string, 0, len(e.Matches))
	for _, match := range e.Matches {
		matches = append(matches, match.Repository.String()+"/"+match.PackageName)
	}
	sort.Strings(matches)
	return fmt.Sprintf("package %q is ambiguous; qualify one of: %s", e.PackageName, strings.Join(matches, ", "))
}

// NewIndex returns an empty catalog index.
func NewIndex() Index {
	return Index{SchemaVersion: schemaVersion}
}

// NewRepositoryRecord creates a catalog record from a verified manifest.
func NewRepositoryRecord(repository verification.Repository, cfg manifest.Config, refreshedAt time.Time) (RepositoryRecord, error) {
	if err := validateRepository(repository); err != nil {
		return RepositoryRecord{}, err
	}
	packages := make([]PackageSummary, 0, len(cfg.Packages))
	for _, pkg := range cfg.Packages {
		packages = append(packages, PackageSummary{
			Name:        pkg.Name,
			Description: strings.TrimSpace(pkg.Description),
			Binaries:    exposedBinaryNames(pkg.Binaries),
		})
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})
	return RepositoryRecord{
		Repository:  repository,
		Packages:    packages,
		RefreshedAt: refreshedAt.UTC(),
	}, nil
}

// Normalize returns a canonical copy of index.
func (i Index) Normalize() Index {
	if i.SchemaVersion == 0 {
		i.SchemaVersion = schemaVersion
	}
	sort.Slice(i.Repositories, func(a, b int) bool {
		return strings.ToLower(i.Repositories[a].Repository.String()) < strings.ToLower(i.Repositories[b].Repository.String())
	})
	for idx := range i.Repositories {
		sort.Slice(i.Repositories[idx].Packages, func(a, b int) bool {
			return i.Repositories[idx].Packages[a].Name < i.Repositories[idx].Packages[b].Name
		})
		for packageIdx := range i.Repositories[idx].Packages {
			sort.Strings(i.Repositories[idx].Packages[packageIdx].Binaries)
		}
	}
	return i
}

// Validate checks the catalog schema and repository records.
func (i Index) Validate() error {
	if i.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported catalog index version %d", i.SchemaVersion)
	}
	seen := map[string]struct{}{}
	for _, record := range i.Repositories {
		if err := validateRepository(record.Repository); err != nil {
			return err
		}
		key := repositoryKey(record.Repository)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("repository %s is indexed more than once", record.Repository)
		}
		seen[key] = struct{}{}
		if len(record.Packages) == 0 {
			return fmt.Errorf("repository %s has no indexed packages", record.Repository)
		}
		for _, pkg := range record.Packages {
			if strings.TrimSpace(pkg.Name) == "" {
				return fmt.Errorf("repository %s has an indexed package without a name", record.Repository)
			}
		}
	}
	return nil
}

// UpsertRepository returns an index with record added or replaced.
func (i Index) UpsertRepository(record RepositoryRecord) (Index, error) {
	if err := validateRepository(record.Repository); err != nil {
		return Index{}, err
	}
	i = i.Normalize()
	key := repositoryKey(record.Repository)
	replaced := false
	for idx, existing := range i.Repositories {
		if repositoryKey(existing.Repository) == key {
			i.Repositories[idx] = record
			replaced = true
			break
		}
	}
	if !replaced {
		i.Repositories = append(i.Repositories, record)
	}
	return i.Normalize(), nil
}

// RemoveRepository returns an index without repository.
func (i Index) RemoveRepository(repository verification.Repository) (Index, bool) {
	i = i.Normalize()
	key := repositoryKey(repository)
	next := i.Repositories[:0]
	removed := false
	for _, record := range i.Repositories {
		if repositoryKey(record.Repository) == key {
			removed = true
			continue
		}
		next = append(next, record)
	}
	i.Repositories = next
	return i.Normalize(), removed
}

// Repository returns one indexed repository record.
func (i Index) Repository(repository verification.Repository) (RepositoryRecord, bool) {
	key := repositoryKey(repository)
	for _, record := range i.Repositories {
		if repositoryKey(record.Repository) == key {
			return record, true
		}
	}
	return RepositoryRecord{}, false
}

// ResolvePackage resolves an unqualified package name through the index.
func (i Index) ResolvePackage(packageName string) (ResolvedPackage, error) {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return ResolvedPackage{}, fmt.Errorf("package name must be set")
	}
	candidates := map[string]ResolvedPackage{}
	for _, record := range i.Repositories {
		for _, pkg := range record.Packages {
			if pkg.Name == packageName || pkg.exposesBinary(packageName) {
				candidate := ResolvedPackage{
					Repository:  record.Repository,
					PackageName: pkg.Name,
				}
				candidates[repositoryKey(record.Repository)+"/"+strings.ToLower(pkg.Name)] = candidate
			}
		}
	}
	matches := make([]ResolvedPackage, 0, len(candidates))
	for _, candidate := range candidates {
		matches = append(matches, candidate)
	}
	sort.Slice(matches, func(a, b int) bool {
		left := strings.ToLower(matches[a].Repository.String() + "/" + matches[a].PackageName)
		right := strings.ToLower(matches[b].Repository.String() + "/" + matches[b].PackageName)
		return left < right
	})
	switch len(matches) {
	case 0:
		return ResolvedPackage{}, fmt.Errorf("package %q is not indexed", packageName)
	case 1:
		return matches[0], nil
	default:
		return ResolvedPackage{}, AmbiguousPackageError{PackageName: packageName, Matches: matches}
	}
}

func (p PackageSummary) exposesBinary(name string) bool {
	for _, binary := range p.Binaries {
		if binary == name {
			return true
		}
	}
	return false
}

func validateRepository(repository verification.Repository) error {
	if strings.TrimSpace(repository.Owner) == "" || strings.TrimSpace(repository.Name) == "" {
		return fmt.Errorf("repository must be owner/repo")
	}
	if strings.Contains(repository.Owner, "/") || strings.Contains(repository.Name, "/") {
		return fmt.Errorf("repository must be owner/repo")
	}
	return nil
}

func repositoryKey(repository verification.Repository) string {
	return strings.ToLower(repository.Owner) + "/" + strings.ToLower(repository.Name)
}

func exposedBinaryNames(binaries []manifest.Binary) []string {
	names := make([]string, 0, len(binaries))
	seen := map[string]struct{}{}
	for _, binary := range binaries {
		normalized := strings.ReplaceAll(strings.TrimSpace(binary.Path), "\\", "/")
		name := path.Base(normalized)
		if name == "" || name == "." || name == "/" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
