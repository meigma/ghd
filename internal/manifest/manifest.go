package manifest

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/meigma/ghd/internal/verification"
)

const (
	// SchemaVersion is the supported ghd.toml schema version.
	SchemaVersion     = 1
	defaultTagPattern = "v${version}"
	versionToken      = "${version}"
)

// PackageName identifies one package within a repository manifest.
type PackageName string

// NewPackageName returns a validated package name.
func NewPackageName(value string) (PackageName, error) {
	name := PackageName(strings.TrimSpace(value))
	if err := name.Validate(); err != nil {
		return "", err
	}
	return name, nil
}

// String returns the raw package name.
func (n PackageName) String() string {
	return string(n)
}

// IsZero reports whether n is unset.
func (n PackageName) IsZero() bool {
	return n == ""
}

// Validate checks that n is a safe manifest package name.
func (n PackageName) Validate() error {
	name := string(n)
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("package name must be set")
	}
	if err := validateNoControlCharacters("package name", name); err != nil {
		return err
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '.', '_', '-':
			continue
		default:
			return fmt.Errorf("package name %q contains unsupported character %q", name, r)
		}
	}
	return nil
}

// PackageVersion identifies a literal package version token used for manifest expansion.
type PackageVersion string

// NewPackageVersion returns a validated package version.
func NewPackageVersion(value string) (PackageVersion, error) {
	version := PackageVersion(strings.TrimSpace(value))
	if err := version.Validate(); err != nil {
		return "", err
	}
	return version, nil
}

// String returns the raw package version.
func (v PackageVersion) String() string {
	return string(v)
}

// IsZero reports whether v is unset.
func (v PackageVersion) IsZero() bool {
	return v == ""
}

// Validate checks that v is safe to use as a literal manifest expansion token.
func (v PackageVersion) Validate() error {
	version := string(v)
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("version must be set")
	}
	if version != strings.TrimSpace(version) {
		return fmt.Errorf("version must not contain leading or trailing whitespace")
	}
	if err := validateNoControlCharacters("version", version); err != nil {
		return err
	}
	if version == "." || version == ".." {
		return fmt.Errorf("version %q must be a path segment", version)
	}
	if strings.ContainsAny(version, `/\`) {
		return fmt.Errorf("version must not contain path separators")
	}
	return nil
}

// Config is the root ghd.toml schema.
type Config struct {
	// Version is the ghd.toml schema version.
	Version int `toml:"version"`
	// Provenance contains repository-wide provenance policy.
	Provenance Provenance `toml:"provenance"`
	// Packages contains installable packages exposed by the repository.
	Packages []Package `toml:"packages"`
}

// Provenance contains repository-wide provenance policy.
type Provenance struct {
	// SignerWorkflow is the trusted GitHub Actions workflow identity.
	SignerWorkflow string `toml:"signer_workflow"`
}

// Package is one installable unit in a repository.
type Package struct {
	// Name is the package name within the repository.
	Name PackageName `toml:"name"`
	// Description is human-readable package text.
	Description string `toml:"description"`
	// TagPattern maps a requested version to a GitHub release tag.
	TagPattern string `toml:"tag_pattern"`
	// Assets maps platform tuples to release asset names.
	Assets []Asset `toml:"assets"`
	// Binaries lists executable paths inside the verified artifact or prepared directory.
	Binaries []Binary `toml:"binaries"`
}

// Asset maps one platform tuple to a release asset name pattern.
type Asset struct {
	// OS is the Go-style target operating system.
	OS string `toml:"os"`
	// Arch is the Go-style target architecture.
	Arch string `toml:"arch"`
	// Pattern maps a requested version to a release asset name.
	Pattern string `toml:"pattern"`
}

// Binary identifies one executable path inside a direct asset or extracted archive.
type Binary struct {
	// Path is a relative executable path inside the verified artifact.
	Path string `toml:"path"`
}

// Platform identifies a Go OS/architecture tuple.
type Platform struct {
	// OS is the Go-style target operating system.
	OS string
	// Arch is the Go-style target architecture.
	Arch string
}

// ResolvedAsset is a platform asset after version pattern expansion.
type ResolvedAsset struct {
	// OS is the selected target operating system.
	OS string
	// Arch is the selected target architecture.
	Arch string
	// Pattern is the manifest pattern that produced Name.
	Pattern string
	// Name is the concrete GitHub release asset name.
	Name string
}

// Decode parses and validates ghd.toml bytes.
func Decode(data []byte) (Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode ghd.toml: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// CurrentPlatform returns the local Go platform tuple.
func CurrentPlatform() Platform {
	return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// WithDefaults fills unset platform fields with the current platform.
func (p Platform) WithDefaults() Platform {
	if p.OS == "" {
		p.OS = runtime.GOOS
	}
	if p.Arch == "" {
		p.Arch = runtime.GOARCH
	}
	return p
}

// Validate checks the manifest schema and policy fields.
func (c Config) Validate() error {
	if c.Version != SchemaVersion {
		return fmt.Errorf("unsupported ghd.toml version %d", c.Version)
	}
	if strings.TrimSpace(c.Provenance.SignerWorkflow) == "" {
		return fmt.Errorf("provenance.signer_workflow must be set")
	}
	if err := validateNoControlCharacters("provenance.signer_workflow", c.Provenance.SignerWorkflow); err != nil {
		return err
	}
	if _, err := c.Provenance.validatedTrustedSignerWorkflow(); err != nil {
		return fmt.Errorf("provenance.signer_workflow: %w", err)
	}
	if len(c.Packages) == 0 {
		return fmt.Errorf("at least one package must be declared")
	}

	seen := map[string]struct{}{}
	for i, pkg := range c.Packages {
		if err := pkg.Validate(); err != nil {
			return fmt.Errorf("packages[%d]: %w", i, err)
		}
		key := strings.ToLower(pkg.Name.String())
		if _, ok := seen[key]; ok {
			return fmt.Errorf("package %q is declared more than once", pkg.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// TrustedSignerWorkflow returns the verification workflow identity.
func (p Provenance) TrustedSignerWorkflow() verification.WorkflowIdentity {
	identity, _ := p.validatedTrustedSignerWorkflow()
	return identity
}

func (p Provenance) validatedTrustedSignerWorkflow() (verification.WorkflowIdentity, error) {
	return verification.NewWorkflowIdentity(p.SignerWorkflow)
}

// Package returns the package with name.
func (c Config) Package(name PackageName) (Package, error) {
	if err := name.Validate(); err != nil {
		return Package{}, err
	}
	for _, pkg := range c.Packages {
		if pkg.Name == name {
			return pkg, nil
		}
	}
	return Package{}, fmt.Errorf("package %q is not declared in ghd.toml", name)
}

// Validate checks one package declaration.
func (p Package) Validate() error {
	if err := p.Name.Validate(); err != nil {
		return err
	}
	if err := validateNoControlCharacters("tag pattern", p.TagPattern); err != nil {
		return err
	}
	if err := validateVersionPattern("tag pattern", p.EffectiveTagPattern()); err != nil {
		return err
	}
	if len(p.Assets) == 0 {
		return fmt.Errorf("package %q must declare at least one asset", p.Name)
	}
	for i, asset := range p.Assets {
		if err := asset.Validate(); err != nil {
			return fmt.Errorf("assets[%d]: %w", i, err)
		}
	}
	if len(p.Binaries) == 0 {
		return fmt.Errorf("package %q must declare at least one binary", p.Name)
	}
	for i, binary := range p.Binaries {
		if err := binary.Validate(); err != nil {
			return fmt.Errorf("binaries[%d]: %w", i, err)
		}
	}
	return nil
}

// EffectiveTagPattern returns the explicit tag pattern or the schema default.
func (p Package) EffectiveTagPattern() string {
	pattern := strings.TrimSpace(p.TagPattern)
	if pattern == "" {
		return defaultTagPattern
	}
	return pattern
}

// ReleaseTag expands the package tag pattern for version.
func (p Package) ReleaseTag(version PackageVersion) (verification.ReleaseTag, error) {
	if err := version.Validate(); err != nil {
		return "", err
	}
	pattern := p.EffectiveTagPattern()
	tag := expandVersion(pattern, version.String())
	if tag == "" {
		return "", fmt.Errorf("release tag pattern for package %q resolved to an empty tag", p.Name)
	}
	releaseTag, err := verification.NewReleaseTag(tag)
	if err != nil {
		return "", fmt.Errorf("release tag pattern for package %q resolved to invalid tag %q: %w", p.Name, tag, err)
	}
	return releaseTag, nil
}

// VersionForTag extracts one package version from tag when it matches TagPattern exactly.
func (p Package) VersionForTag(tag verification.ReleaseTag) (PackageVersion, bool, error) {
	if err := tag.Validate(); err != nil {
		return "", false, err
	}
	prefix, suffix, err := versionPatternParts(strings.TrimSpace(p.TagPattern))
	if err != nil {
		return "", false, err
	}
	value := tag.String()
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false, nil
	}
	version := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	if version == "" {
		return "", false, nil
	}
	packageVersion, err := NewPackageVersion(version)
	if err != nil {
		return "", false, err
	}
	return packageVersion, true, nil
}

// SelectAsset returns the single asset matching platform.
func (p Package) SelectAsset(platform Platform, version PackageVersion) (ResolvedAsset, error) {
	platform = platform.WithDefaults()
	if platform.OS == "" || platform.Arch == "" {
		return ResolvedAsset{}, fmt.Errorf("platform OS and architecture must be set")
	}

	var matches []Asset
	for _, asset := range p.Assets {
		if strings.EqualFold(asset.OS, platform.OS) && strings.EqualFold(asset.Arch, platform.Arch) {
			matches = append(matches, asset)
		}
	}
	switch len(matches) {
	case 0:
		return ResolvedAsset{}, fmt.Errorf("package %q has no asset for %s/%s", p.Name, platform.OS, platform.Arch)
	case 1:
		name, err := matches[0].ResolveName(version)
		if err != nil {
			return ResolvedAsset{}, err
		}
		return ResolvedAsset{
			OS:      matches[0].OS,
			Arch:    matches[0].Arch,
			Pattern: matches[0].Pattern,
			Name:    name,
		}, nil
	default:
		return ResolvedAsset{}, fmt.Errorf("package %q has multiple assets for %s/%s", p.Name, platform.OS, platform.Arch)
	}
}

// Validate checks one asset declaration.
func (a Asset) Validate() error {
	if strings.TrimSpace(a.OS) == "" {
		return fmt.Errorf("os must be set")
	}
	if err := validateNoControlCharacters("os", a.OS); err != nil {
		return err
	}
	if strings.TrimSpace(a.Arch) == "" {
		return fmt.Errorf("arch must be set")
	}
	if err := validateNoControlCharacters("arch", a.Arch); err != nil {
		return err
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return fmt.Errorf("pattern must be set")
	}
	if err := validateNoControlCharacters("pattern", a.Pattern); err != nil {
		return err
	}
	if err := validateVersionPattern("asset pattern", strings.TrimSpace(a.Pattern)); err != nil {
		return err
	}
	return nil
}

// ResolveName expands the asset pattern for version.
func (a Asset) ResolveName(version PackageVersion) (string, error) {
	if err := version.Validate(); err != nil {
		return "", err
	}
	name := expandVersion(strings.TrimSpace(a.Pattern), version.String())
	if name == "" {
		return "", fmt.Errorf("asset pattern for %s/%s resolved to an empty name", a.OS, a.Arch)
	}
	return name, nil
}

// Validate checks one binary declaration.
func (b Binary) Validate() error {
	binaryPath := strings.TrimSpace(b.Path)
	if binaryPath == "" {
		return fmt.Errorf("path must be set")
	}
	if err := validateNoControlCharacters("binary path", b.Path); err != nil {
		return err
	}
	normalized := strings.ReplaceAll(binaryPath, "\\", "/")
	if path.IsAbs(normalized) || filepath.IsAbs(binaryPath) {
		return fmt.Errorf("binary path %q must be relative", b.Path)
	}
	clean := path.Clean(normalized)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("binary path %q must not contain ..", b.Path)
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return fmt.Errorf("binary path %q must not contain ..", b.Path)
		}
	}
	return nil
}

func expandVersion(pattern string, version string) string {
	return strings.ReplaceAll(pattern, versionToken, version)
}

func versionPatternParts(pattern string) (string, string, error) {
	if pattern == "" {
		pattern = defaultTagPattern
	}
	if err := validateVersionPattern("tag pattern", pattern); err != nil {
		return "", "", err
	}
	prefix, suffix, _ := strings.Cut(pattern, versionToken)
	return prefix, suffix, nil
}

func validateVersionPattern(label string, pattern string) error {
	if strings.Count(pattern, versionToken) != 1 {
		return fmt.Errorf("%s %q must contain exactly one %s token", label, pattern, versionToken)
	}
	if err := validateNoControlCharacters(label, pattern); err != nil {
		return err
	}
	return nil
}

func validateNoControlCharacters(label string, value string) error {
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain control characters", label)
		}
	}
	return nil
}
