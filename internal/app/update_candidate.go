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
	CandidateAsset manifest.Asset
	LatestVersion  manifest.PackageVersion
	Tag            verification.ReleaseTag
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
	packageName, err := manifest.NewPackageName(record.Package)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}
	version, err := manifest.NewPackageVersion(record.Version)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}

	installedCfg, installedPkg, err := fetchPackageManifestForVersionAtTag(ctx, manifests, repository, packageName, version, verification.ReleaseTag(record.Tag))
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}
	installedAsset, err := installedAssetDeclaration(installedPkg, record)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}

	repositoryReleases, err := releases.ListRepositoryReleases(ctx, repository)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, fmt.Errorf("list GitHub releases: %w", err)
	}
	candidate, err := latestStablePackageUpdate(ctx, manifests, repository, packageName, installedAsset, repositoryReleases, installedVersion)
	if err != nil {
		return resolvedInstalledPackageUpdate{}, err
	}
	if candidate.LatestVersion.IsZero() {
		return resolvedInstalledPackageUpdate{
			Repository:     repository,
			Config:         installedCfg,
			Package:        installedPkg,
			InstalledAsset: installedAsset,
		}, nil
	}

	return candidate, nil
}
