package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/verification"
)

func TestRenderPackageListTTYGroupsByRepository(t *testing.T) {
	got := renderPackageListTTY([]app.PackageListResult{
		{
			Repository:  verification.Repository{Owner: "owner", Name: "alpha"},
			PackageName: "bar",
			Binaries:    []string{"bar"},
		},
		{
			Repository:  verification.Repository{Owner: "owner", Name: "alpha"},
			PackageName: "foo",
			Binaries:    []string{"foo", "fooctl"},
		},
		{
			Repository:  verification.Repository{Owner: "owner", Name: "beta"},
			PackageName: "baz",
			Binaries:    []string{"baz"},
		},
	}, verification.Repository{}, false)

	assert.Contains(t, got, "indexed packages")
	assert.Contains(t, got, "owner/alpha")
	assert.Contains(t, got, "owner/beta")
	assert.Contains(t, got, "foo, fooctl")
	assert.Contains(t, got, "2 repositories, 3 packages")
}

func TestRenderPackageInfoTTYIncludesAssetTable(t *testing.T) {
	got := renderPackageInfoTTY(app.PackageInfoResult{
		Repository:     verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:    "foo",
		SignerWorkflow: verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"),
		TagPattern:     "v${version}",
		Binaries:       []string{"foo"},
		Assets: []app.PackageInfoAsset{
			{OS: "darwin", Arch: "arm64", Pattern: "foo_${version}_darwin_arm64.tar.gz"},
			{OS: "linux", Arch: "amd64", Pattern: "foo_${version}_linux_amd64.tar.gz"},
		},
	}, false)

	assert.Contains(t, got, "package owner/repo/foo")
	assert.Contains(t, got, "Signer:")
	assert.Contains(t, got, "assets")
	assert.Contains(t, got, "darwin/arm64")
	assert.Contains(t, got, "foo_${version}_linux_amd64.tar.gz")
}

func TestRenderCheckResultsTTYGroupsStatuses(t *testing.T) {
	got := renderCheckResultsTTY([]app.CheckResult{
		{
			Repository:    "owner/repo",
			Package:       "foo",
			Version:       "1.2.3",
			Status:        app.CheckStatusUpdateAvailable,
			LatestVersion: "1.3.0",
		},
		{
			Repository: "owner/current",
			Package:    "foo",
			Version:    "1.2.3",
			Status:     app.CheckStatusUpToDate,
		},
		{
			Repository: "owner/broken",
			Package:    "foo",
			Version:    "1.2.3",
			Status:     app.CheckStatusCannotDetermine,
			Reason:     "fetch ghd.toml: missing",
		},
	}, false)

	assert.Contains(t, got, "update check")
	assert.Contains(t, got, "updates available")
	assert.Contains(t, got, "1.2.3 -> 1.3.0")
	assert.Contains(t, got, "current")
	assert.Contains(t, got, "could not determine")
	assert.Contains(t, got, "summary")
	assert.Contains(t, got, "Updates: 1")
	assert.Contains(t, got, "Failed: 1")
}
