package app

import (
	"context"
	"fmt"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

type resolvedInstalledPackageUpdate struct {
	Repository     verification.Repository
	Config         manifest.Config
	Package        manifest.Package
	InstalledAsset manifest.Asset
	LatestVersion  string
}

func resolveInstalledPackageUpdate(ctx context.Context, manifests ManifestSource, releases RepositoryReleaseSource, record state.Record) (resolvedInstalledPackageUpdate, error) {
	repository, err := parseRecordRepository(record.Repository)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}
	installedVersion, err := normalizeSemver(record.Version)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, fmt.Errorf("installed version %q is not a supported semantic version", record.Version)
	}

	manifestBytes, err := manifests.FetchManifest(ctx, repository)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, fmt.Errorf("fetch ghd.toml: %w", err)
	}
	cfg, err := manifest.Decode(manifestBytes)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}
	pkg, err := cfg.Package(record.Package)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}
	installedAsset, err := installedAssetDeclaration(pkg, record)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}

	repositoryReleases, err := releases.ListRepositoryReleases(ctx, repository)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, fmt.Errorf("list GitHub releases: %w", err)
	}
	latestVersion, err := latestStableVersion(pkg, installedAsset, repositoryReleases, installedVersion)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}

	return resolvedInstalledPackageUpdate{
		Repository:     repository,
		Config:         cfg,
		Package:        pkg,
		InstalledAsset: installedAsset,
		LatestVersion:  latestVersion,
	}, nil
}
