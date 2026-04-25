package app

import (
	"context"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

type latestStablePackageRelease struct {
	Config    manifest.Config
	Package   manifest.Package
	Version   manifest.PackageVersion
	Tag       verification.ReleaseTag
	Asset     manifest.Asset
	AssetName string
}

func latestStablePackageReleaseForPlatform(
	ctx context.Context,
	manifests ManifestSource,
	repository verification.Repository,
	packageName manifest.PackageName,
	releases []RepositoryRelease,
	platform manifest.Platform,
	minimumVersion string,
) (latestStablePackageRelease, error) {
	platform = platform.WithDefaults()
	var best latestStablePackageRelease
	bestSemver := ""
	for _, release := range releases {
		if err := ctx.Err(); err != nil {
			return latestStablePackageRelease{}, err
		}
		if release.Draft || release.Prerelease || strings.TrimSpace(release.TagName) == "" {
			continue
		}
		tag := verification.ReleaseTag(release.TagName)
		cfg, pkg, version, err := fetchPackageManifestForReleaseTag(ctx, manifests, repository, packageName, tag)
		if err != nil {
			continue
		}
		releaseVersion, err := normalizeStableSemver(version.String())
		if err != nil {
			continue
		}
		if minimumVersion != "" && semver.Compare(releaseVersion, minimumVersion) <= 0 {
			continue
		}
		asset, assetName, ok := releaseAssetForPlatform(pkg, version, platform, release)
		if !ok {
			continue
		}
		if bestSemver == "" || semver.Compare(releaseVersion, bestSemver) > 0 {
			bestSemver = releaseVersion
			best = latestStablePackageRelease{
				Config:    cfg,
				Package:   pkg,
				Version:   version,
				Tag:       tag,
				Asset:     asset,
				AssetName: assetName,
			}
		}
	}
	return best, nil
}

func releaseAssetForPlatform(
	pkg manifest.Package,
	version manifest.PackageVersion,
	platform manifest.Platform,
	release RepositoryRelease,
) (manifest.Asset, string, bool) {
	asset, assetName, err := resolvedAssetForPlatform(pkg, version, platform)
	if err != nil {
		return manifest.Asset{}, "", false
	}
	if !release.hasAsset(assetName) {
		return manifest.Asset{}, "", false
	}
	return asset, assetName, true
}

func resolvedAssetForPlatform(pkg manifest.Package, version manifest.PackageVersion, platform manifest.Platform) (manifest.Asset, string, error) {
	asset, err := assetForPlatform(pkg, platform)
	if err != nil {
		return manifest.Asset{}, "", err
	}
	assetName, err := asset.ResolveName(version)
	if err != nil {
		return manifest.Asset{}, "", err
	}
	return asset, assetName, nil
}
