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
	Name string `toml:"name"`
	// Description is human-readable package text.
	Description string `toml:"description"`
	// TagPattern maps a requested version to a GitHub release tag.
	TagPattern string `toml:"tag_pattern"`
	// Assets maps platform tuples to release asset names.
	Assets []Asset `toml:"assets"`
	// Binaries lists executable paths inside the verified asset.
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

// Binary identifies one executable path inside an asset or extracted archive.
type Binary struct {
	// Path is a relative executable path inside the verified asset.
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
	if len(c.Packages) == 0 {
		return fmt.Errorf("at least one package must be declared")
	}

	seen := map[string]struct{}{}
	for i, pkg := range c.Packages {
		if err := pkg.Validate(); err != nil {
			return fmt.Errorf("packages[%d]: %w", i, err)
		}
		key := strings.ToLower(pkg.Name)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("package %q is declared more than once", pkg.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// TrustedSignerWorkflow returns the verification workflow identity.
func (p Provenance) TrustedSignerWorkflow() verification.WorkflowIdentity {
	return verification.WorkflowIdentity(strings.TrimSpace(p.SignerWorkflow))
}

// Package returns the package with name.
func (c Config) Package(name string) (Package, error) {
	name = strings.TrimSpace(name)
	for _, pkg := range c.Packages {
		if pkg.Name == name {
			return pkg, nil
		}
	}
	return Package{}, fmt.Errorf("package %q is not declared in ghd.toml", name)
}

// Validate checks one package declaration.
func (p Package) Validate() error {
	if err := validatePackageName(p.Name); err != nil {
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

// ReleaseTag expands the package tag pattern for version.
func (p Package) ReleaseTag(version string) (verification.ReleaseTag, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("version must be set")
	}
	pattern := strings.TrimSpace(p.TagPattern)
	if pattern == "" {
		pattern = defaultTagPattern
	}
	tag := expandVersion(pattern, version)
	if tag == "" {
		return "", fmt.Errorf("release tag pattern for package %q resolved to an empty tag", p.Name)
	}
	return verification.ReleaseTag(tag), nil
}

// SelectAsset returns the single asset matching platform.
func (p Package) SelectAsset(platform Platform, version string) (ResolvedAsset, error) {
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
	if strings.TrimSpace(a.Arch) == "" {
		return fmt.Errorf("arch must be set")
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return fmt.Errorf("pattern must be set")
	}
	return nil
}

// ResolveName expands the asset pattern for version.
func (a Asset) ResolveName(version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("version must be set")
	}
	name := expandVersion(strings.TrimSpace(a.Pattern), version)
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

func validatePackageName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("package name must be set")
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

func expandVersion(pattern string, version string) string {
	return strings.ReplaceAll(pattern, versionToken, version)
}
