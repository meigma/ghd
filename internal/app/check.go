package app

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

// RepositoryRelease describes one GitHub release used for update discovery.
type RepositoryRelease struct {
	// TagName is the Git tag name attached to the release.
	TagName string
	// Draft reports whether the release is still a draft.
	Draft bool
	// Prerelease reports whether the release is marked as a prerelease.
	Prerelease bool
	// AssetNames are the release asset names published for TagName.
	AssetNames []string
}

// RepositoryReleaseSource lists repository releases for update discovery.
type RepositoryReleaseSource interface {
	// ListRepositoryReleases returns the repository's GitHub releases.
	ListRepositoryReleases(ctx context.Context, repository verification.Repository) ([]RepositoryRelease, error)
}

// InstalledStateReader loads active installed package state.
type InstalledStateReader interface {
	// LoadInstalledState reads active installed package state from stateDir.
	LoadInstalledState(ctx context.Context, stateDir string) (state.Index, error)
}

// InstalledPackageCheckerDependencies contains the ports needed by InstalledPackageChecker.
type InstalledPackageCheckerDependencies struct {
	// Manifests fetches repository manifest bytes.
	Manifests ManifestSource
	// Releases lists repository releases for update discovery.
	Releases RepositoryReleaseSource
	// StateStore loads active installed package records.
	StateStore InstalledStateReader
}

// CheckRequest describes one installed-package update check.
type CheckRequest struct {
	// Target is an optional package name, binary name, or owner/repo/package target.
	Target string
	// All checks every active installed package.
	All bool
	// StateDir stores active installed package state.
	StateDir string
}

// CheckStatus describes the update state of one installed package.
type CheckStatus string

const (
	// CheckStatusUpToDate reports that no newer matching stable release was found.
	CheckStatusUpToDate CheckStatus = "up-to-date"
	// CheckStatusUpdateAvailable reports that a newer matching stable release exists.
	CheckStatusUpdateAvailable CheckStatus = "update-available"
	// CheckStatusCannotDetermine reports that update discovery could not complete.
	CheckStatusCannotDetermine CheckStatus = "cannot-determine"
)

// CheckResult describes the update state for one installed package.
type CheckResult struct {
	// Repository is the GitHub repository that owns the package.
	Repository string
	// Package is the installed package name.
	Package string
	// Version is the installed package version.
	Version string
	// Status is the update state for the installed package.
	Status CheckStatus
	// LatestVersion is the newest discovered version when Status is update-available.
	LatestVersion string
	// Reason explains why update discovery could not complete.
	Reason string
}

// CheckIncompleteError reports that one or more package checks could not complete.
type CheckIncompleteError struct {
	// Failed is the number of packages whose checks failed.
	Failed int
}

// Error describes the aggregated check failure.
func (e CheckIncompleteError) Error() string {
	if e.Failed == 1 {
		return "could not determine updates for 1 installed package"
	}
	return fmt.Sprintf("could not determine updates for %d installed packages", e.Failed)
}

// InstalledPackageChecker implements read-only installed package update checks.
type InstalledPackageChecker struct {
	manifests ManifestSource
	releases  RepositoryReleaseSource
	state     InstalledStateReader
}

// NewInstalledPackageChecker creates an installed package checker use case.
func NewInstalledPackageChecker(deps InstalledPackageCheckerDependencies) (*InstalledPackageChecker, error) {
	if deps.Manifests == nil {
		return nil, fmt.Errorf("manifest source must be set")
	}
	if deps.Releases == nil {
		return nil, fmt.Errorf("release source must be set")
	}
	if deps.StateStore == nil {
		return nil, fmt.Errorf("installed state store must be set")
	}
	return &InstalledPackageChecker{
		manifests: deps.Manifests,
		releases:  deps.Releases,
		state:     deps.StateStore,
	}, nil
}

// Check reports update availability for one installed target or every installed package.
func (c *InstalledPackageChecker) Check(ctx context.Context, request CheckRequest) ([]CheckResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	index, err := c.state.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return nil, err
	}

	records, err := checkTargets(index.Normalize(), request)
	if err != nil {
		return nil, err
	}

	results := make([]CheckResult, 0, len(records))
	failures := 0
	for _, record := range records {
		result, err := c.checkRecord(ctx, record)
		if err != nil {
			if !request.All {
				return nil, err
			}
			results = append(results, cannotDetermineResult(record, err))
			failures++
			continue
		}
		results = append(results, result)
	}
	if failures != 0 {
		return results, CheckIncompleteError{Failed: failures}
	}
	return results, nil
}

func (c *InstalledPackageChecker) checkRecord(ctx context.Context, record state.Record) (CheckResult, error) {
	result := CheckResult{
		Repository: record.Repository,
		Package:    record.Package,
		Version:    record.Version,
	}
	repository, err := parseRecordRepository(record.Repository)
	if err != nil {
		return CheckResult{}, err
	}
	installedVersion, err := normalizeSemver(record.Version)
	if err != nil {
		return CheckResult{}, fmt.Errorf("installed version %q is not a supported semantic version", record.Version)
	}

	manifestBytes, err := c.manifests.FetchManifest(ctx, repository)
	if err != nil {
		return CheckResult{}, fmt.Errorf("fetch ghd.toml: %w", err)
	}
	cfg, err := manifest.Decode(manifestBytes)
	if err != nil {
		return CheckResult{}, err
	}
	pkg, err := cfg.Package(record.Package)
	if err != nil {
		return CheckResult{}, err
	}
	installedAsset, err := installedAssetDeclaration(pkg, record)
	if err != nil {
		return CheckResult{}, err
	}

	releases, err := c.releases.ListRepositoryReleases(ctx, repository)
	if err != nil {
		return CheckResult{}, fmt.Errorf("list GitHub releases: %w", err)
	}
	latestVersion, err := latestStableVersion(pkg, installedAsset, releases, installedVersion)
	if err != nil {
		return CheckResult{}, err
	}

	if latestVersion == "" {
		result.Status = CheckStatusUpToDate
		return result, nil
	}
	result.Status = CheckStatusUpdateAvailable
	result.LatestVersion = latestVersion
	return result, nil
}

func (r CheckRequest) validate() error {
	if strings.TrimSpace(r.StateDir) == "" {
		return fmt.Errorf("state directory must be set")
	}
	if r.All && strings.TrimSpace(r.Target) != "" {
		return fmt.Errorf("check accepts a target or --all, not both")
	}
	if !r.All && strings.TrimSpace(r.Target) == "" {
		return fmt.Errorf("check target must be set")
	}
	return nil
}

func checkTargets(index state.Index, request CheckRequest) ([]state.Record, error) {
	if request.All {
		return index.Normalize().Records, nil
	}
	record, err := index.ResolveTarget(request.Target)
	if err != nil {
		return nil, err
	}
	return []state.Record{record}, nil
}

func cannotDetermineResult(record state.Record, err error) CheckResult {
	return CheckResult{
		Repository: record.Repository,
		Package:    record.Package,
		Version:    record.Version,
		Status:     CheckStatusCannotDetermine,
		Reason:     err.Error(),
	}
}

func parseRecordRepository(value string) (verification.Repository, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return verification.Repository{}, fmt.Errorf("repository must be owner/repo")
	}
	return verification.Repository{Owner: parts[0], Name: parts[1]}, nil
}

func latestStableVersion(pkg manifest.Package, installedAsset manifest.Asset, releases []RepositoryRelease, installedVersion string) (string, error) {
	bestVersion := ""
	bestSemver := ""
	for _, release := range releases {
		if release.Draft || release.Prerelease || strings.TrimSpace(release.TagName) == "" {
			continue
		}
		version, matched, err := pkg.VersionForTag(verification.ReleaseTag(release.TagName))
		if err != nil {
			return "", err
		}
		if !matched {
			continue
		}
		releaseVersion, err := normalizeStableSemver(version)
		if err != nil {
			continue
		}
		candidateAsset, err := installedAsset.ResolveName(version)
		if err != nil {
			return "", err
		}
		if !release.hasAsset(candidateAsset) {
			continue
		}
		if semver.Compare(releaseVersion, installedVersion) <= 0 {
			continue
		}
		if bestSemver == "" || semver.Compare(releaseVersion, bestSemver) > 0 {
			bestVersion = version
			bestSemver = releaseVersion
		}
	}
	return bestVersion, nil
}

func installedAssetDeclaration(pkg manifest.Package, record state.Record) (manifest.Asset, error) {
	matches := make([]manifest.Asset, 0, 1)
	for _, asset := range pkg.Assets {
		name, err := asset.ResolveName(record.Version)
		if err != nil {
			return manifest.Asset{}, err
		}
		if name == record.Asset {
			matches = append(matches, asset)
		}
	}
	switch len(matches) {
	case 0:
		return manifest.Asset{}, fmt.Errorf("installed asset %q is not declared for %s/%s@%s", record.Asset, record.Repository, record.Package, record.Version)
	case 1:
		return matches[0], nil
	default:
		return manifest.Asset{}, fmt.Errorf("installed asset %q is declared more than once for %s/%s@%s", record.Asset, record.Repository, record.Package, record.Version)
	}
}

func (r RepositoryRelease) hasAsset(name string) bool {
	for _, asset := range r.AssetNames {
		if asset == name {
			return true
		}
	}
	return false
}

func normalizeSemver(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("version must be set")
	}
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	if !semver.IsValid(value) {
		return "", fmt.Errorf("version %q is not valid semver", value)
	}
	return value, nil
}

func normalizeStableSemver(value string) (string, error) {
	value, err := normalizeSemver(value)
	if err != nil {
		return "", err
	}
	if semver.Prerelease(value) != "" {
		return "", fmt.Errorf("version %q is a prerelease", value)
	}
	return value, nil
}
