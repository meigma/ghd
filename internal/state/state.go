package state

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const schemaVersion = 1

// Index is the persisted active install state.
type Index struct {
	// SchemaVersion is the installed state schema version.
	SchemaVersion int `json:"schema_version"`
	// Records are active installed packages.
	Records []Record `json:"records"`
}

// Record describes one active installed package.
type Record struct {
	// Repository is the GitHub repository that owns the package.
	Repository string `json:"repository"`
	// Package is the installed package name.
	Package string `json:"package"`
	// Version is the installed package version.
	Version string `json:"version"`
	// Tag is the resolved release tag.
	Tag string `json:"tag"`
	// Asset is the verified release asset name.
	Asset string `json:"asset"`
	// AssetDigest is the verified artifact digest.
	AssetDigest string `json:"asset_digest"`
	// StorePath is the digest-keyed store directory.
	StorePath string `json:"store_path"`
	// ArtifactPath is the copied artifact path inside the store.
	ArtifactPath string `json:"artifact_path"`
	// ExtractedPath is the extracted archive directory.
	ExtractedPath string `json:"extracted_path"`
	// VerificationPath is the verification evidence path.
	VerificationPath string `json:"verification_path"`
	// Binaries are the exposed installed binaries.
	Binaries []Binary `json:"binaries"`
	// InstalledAt is when the package was recorded as installed.
	InstalledAt time.Time `json:"installed_at"`
}

// Binary describes one exposed binary link.
type Binary struct {
	// Name is the exposed command name.
	Name string `json:"name"`
	// LinkPath is the managed bin path.
	LinkPath string `json:"link_path"`
	// TargetPath is the verified extracted binary path.
	TargetPath string `json:"target_path"`
}

// DuplicateInstallError reports an active install for the same package.
type DuplicateInstallError struct {
	// Repository is the package repository.
	Repository string
	// Package is the package name.
	Package string
}

// NotInstalledError reports a target with no active install.
type NotInstalledError struct {
	// Target is the requested uninstall target.
	Target string
}

// AmbiguousInstallError reports an unqualified target with multiple active installs.
type AmbiguousInstallError struct {
	// Target is the requested uninstall target.
	Target string
	// Matches are active installs matching Target.
	Matches []Record
}

// Error describes the duplicate active install.
func (e DuplicateInstallError) Error() string {
	return fmt.Sprintf("package %s/%s is already installed", e.Repository, e.Package)
}

// Error describes the missing active install.
func (e NotInstalledError) Error() string {
	return fmt.Sprintf("package %q is not installed", e.Target)
}

// Error describes the ambiguous active install lookup.
func (e AmbiguousInstallError) Error() string {
	matches := make([]string, 0, len(e.Matches))
	for _, match := range e.Matches {
		matches = append(matches, match.Repository+"/"+match.Package)
	}
	sort.Strings(matches)
	return fmt.Sprintf("installed package %q is ambiguous; qualify one of: %s", e.Target, strings.Join(matches, ", "))
}

// NewIndex returns an empty installed-state index.
func NewIndex() Index {
	return Index{SchemaVersion: schemaVersion}
}

// Normalize returns a canonical copy of index.
func (i Index) Normalize() Index {
	if i.SchemaVersion == 0 {
		i.SchemaVersion = schemaVersion
	}
	for idx := range i.Records {
		sort.Slice(i.Records[idx].Binaries, func(a, b int) bool {
			left := binarySortKey(i.Records[idx].Binaries[a])
			right := binarySortKey(i.Records[idx].Binaries[b])
			return left < right
		})
	}
	sort.Slice(i.Records, func(a, b int) bool {
		left := recordKey(i.Records[a].Repository, i.Records[a].Package)
		right := recordKey(i.Records[b].Repository, i.Records[b].Package)
		return left < right
	})
	return i
}

// Validate checks the installed-state schema and records.
func (i Index) Validate() error {
	if i.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported installed state version %d", i.SchemaVersion)
	}
	seen := map[string]struct{}{}
	for _, record := range i.Records {
		if err := record.Validate(); err != nil {
			return err
		}
		key := recordKey(record.Repository, record.Package)
		if _, ok := seen[key]; ok {
			return DuplicateInstallError{Repository: record.Repository, Package: record.Package}
		}
		seen[key] = struct{}{}
	}
	return nil
}

// AddRecord returns an index with record added.
func (i Index) AddRecord(record Record) (Index, error) {
	if err := record.Validate(); err != nil {
		return Index{}, err
	}
	i = i.Normalize()
	if _, ok := i.Record(record.Repository, record.Package); ok {
		return Index{}, DuplicateInstallError{Repository: record.Repository, Package: record.Package}
	}
	i.Records = append(i.Records, record)
	i = i.Normalize()
	if err := i.Validate(); err != nil {
		return Index{}, err
	}
	return i, nil
}

// RemoveRecord returns an index with one active installed package removed.
func (i Index) RemoveRecord(repository string, packageName string) (Index, Record, error) {
	target := strings.TrimSpace(repository) + "/" + strings.TrimSpace(packageName)
	i = i.Normalize()
	next := i.Records[:0]
	var removed Record
	found := false
	key := recordKey(repository, packageName)
	for _, record := range i.Records {
		if recordKey(record.Repository, record.Package) == key {
			removed = record
			found = true
			continue
		}
		next = append(next, record)
	}
	if !found {
		return Index{}, Record{}, NotInstalledError{Target: target}
	}
	i.Records = next
	i = i.Normalize()
	if err := i.Validate(); err != nil {
		return Index{}, Record{}, err
	}
	return i, removed, nil
}

// ReplaceRecord returns an index with one active installed package replaced.
func (i Index) ReplaceRecord(record Record) (Index, error) {
	if err := record.Validate(); err != nil {
		return Index{}, err
	}
	i = i.Normalize()
	key := recordKey(record.Repository, record.Package)
	found := false
	for idx, existing := range i.Records {
		if recordKey(existing.Repository, existing.Package) == key {
			i.Records[idx] = record
			found = true
			break
		}
	}
	if !found {
		return Index{}, NotInstalledError{Target: strings.TrimSpace(record.Repository) + "/" + strings.TrimSpace(record.Package)}
	}
	i = i.Normalize()
	if err := i.Validate(); err != nil {
		return Index{}, err
	}
	return i, nil
}

// Record returns one active installed package.
func (i Index) Record(repository string, packageName string) (Record, bool) {
	key := recordKey(repository, packageName)
	for _, record := range i.Records {
		if recordKey(record.Repository, record.Package) == key {
			return record, true
		}
	}
	return Record{}, false
}

// ResolveTarget resolves a user-facing uninstall target to one active install.
func (i Index) ResolveTarget(target string) (Record, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Record{}, fmt.Errorf("uninstall target must be set")
	}
	if strings.Contains(target, "/") {
		repository, packageName, err := splitQualifiedTarget(target)
		if err != nil {
			return Record{}, err
		}
		record, ok := i.Record(repository, packageName)
		if !ok {
			return Record{}, NotInstalledError{Target: target}
		}
		return record, nil
	}

	matchesByKey := map[string]Record{}
	for _, record := range i.Normalize().Records {
		if record.Package == target || record.exposesBinary(target) {
			matchesByKey[recordKey(record.Repository, record.Package)] = record
		}
	}
	matches := make([]Record, 0, len(matchesByKey))
	for _, record := range matchesByKey {
		matches = append(matches, record)
	}
	sort.Slice(matches, func(a, b int) bool {
		left := recordKey(matches[a].Repository, matches[a].Package)
		right := recordKey(matches[b].Repository, matches[b].Package)
		return left < right
	})
	switch len(matches) {
	case 0:
		return Record{}, NotInstalledError{Target: target}
	case 1:
		return matches[0], nil
	default:
		return Record{}, AmbiguousInstallError{Target: target, Matches: matches}
	}
}

// Validate checks one installed package record.
func (r Record) Validate() error {
	if err := validateRepository(r.Repository); err != nil {
		return err
	}
	if strings.TrimSpace(r.Package) == "" {
		return fmt.Errorf("installed package name must be set")
	}
	if strings.Contains(r.Package, "/") {
		return fmt.Errorf("installed package name %q must not contain path separators", r.Package)
	}
	required := []struct {
		label string
		value string
	}{
		{label: "version", value: r.Version},
		{label: "tag", value: r.Tag},
		{label: "asset", value: r.Asset},
		{label: "asset digest", value: r.AssetDigest},
		{label: "store path", value: r.StorePath},
		{label: "artifact path", value: r.ArtifactPath},
		{label: "extracted path", value: r.ExtractedPath},
		{label: "verification path", value: r.VerificationPath},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("installed %s must be set", field.label)
		}
	}
	if r.InstalledAt.IsZero() {
		return fmt.Errorf("installed timestamp must be set")
	}
	if len(r.Binaries) == 0 {
		return fmt.Errorf("installed package must expose at least one binary")
	}
	for _, binary := range r.Binaries {
		if err := binary.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks one installed binary record.
func (b Binary) Validate() error {
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("installed binary name must be set")
	}
	if strings.ContainsAny(b.Name, `/\`) {
		return fmt.Errorf("installed binary name %q must not contain path separators", b.Name)
	}
	if strings.TrimSpace(b.LinkPath) == "" {
		return fmt.Errorf("installed binary link path must be set")
	}
	if strings.TrimSpace(b.TargetPath) == "" {
		return fmt.Errorf("installed binary target path must be set")
	}
	return nil
}

func (r Record) exposesBinary(name string) bool {
	for _, binary := range r.Binaries {
		if binary.Name == name {
			return true
		}
	}
	return false
}

func validateRepository(repository string) error {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return fmt.Errorf("installed repository must be set")
	}
	owner, name, ok := strings.Cut(repository, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
		return fmt.Errorf("installed repository must be owner/repo")
	}
	return nil
}

func splitQualifiedTarget(target string) (string, string, error) {
	parts := strings.Split(target, "/")
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "", "", fmt.Errorf("uninstall target must be name or owner/repo/package")
	}
	return strings.TrimSpace(parts[0]) + "/" + strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), nil
}

func recordKey(repository string, packageName string) string {
	return strings.ToLower(strings.TrimSpace(repository)) + "/" + strings.ToLower(strings.TrimSpace(packageName))
}

func binarySortKey(binary Binary) string {
	return strings.ToLower(binary.Name) + "\x00" + strings.ToLower(binary.LinkPath) + "\x00" + strings.ToLower(binary.TargetPath)
}
