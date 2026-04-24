package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/catalog"
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

func TestRenderRepositoryListTTYIncludesDescriptionsAndSummary(t *testing.T) {
	got := renderRepositoryListTTY([]catalog.RepositoryRecord{
		{
			Repository:  verification.Repository{Owner: "owner", Name: "alpha"},
			RefreshedAt: time.Unix(1700000000, 0).UTC(),
			Packages: []catalog.PackageSummary{
				{Name: "bar", Description: "Bar CLI", Binaries: []string{"bar"}},
				{Name: "foo", Description: "Foo CLI", Binaries: []string{"foo", "fooctl"}},
			},
		},
		{
			Repository:  verification.Repository{Owner: "owner", Name: "beta"},
			RefreshedAt: time.Unix(1700000300, 0).UTC(),
			Packages: []catalog.PackageSummary{
				{Name: "baz", Binaries: []string{"baz"}},
			},
		},
	}, false)

	assert.Contains(t, got, "indexed repositories")
	assert.Contains(t, got, "owner/alpha")
	assert.Contains(t, got, "Refreshed:")
	assert.Contains(t, got, "Foo CLI")
	assert.Contains(t, got, "foo, fooctl")
	assert.Contains(t, got, "2 repositories, 3 packages")
}

func TestRenderVerifyResultsTTYGroupsStatuses(t *testing.T) {
	got := renderVerifyResultsTTY([]app.VerifyInstalledResult{
		{
			Repository: "owner/repo",
			Package:    "foo",
			Version:    "1.2.3",
			Status:     app.VerifyStatusVerified,
		},
		{
			Repository: "owner/broken",
			Package:    "bar",
			Version:    "1.0.0",
			Status:     app.VerifyStatusCannotVerify,
			Reason:     "installed binary \"bar\" does not match verified artifact",
		},
	}, false)

	assert.Contains(t, got, "verification")
	assert.Contains(t, got, "verified")
	assert.Contains(t, got, "could not verify")
	assert.Contains(t, got, "owner/repo/foo  1.2.3")
	assert.Contains(t, got, "owner/broken/bar  1.0.0  installed binary")
	assert.Contains(t, got, "Verified: 1")
	assert.Contains(t, got, "Failed:")
}

func TestRenderDoctorResultsTTYGroupsStatuses(t *testing.T) {
	got := renderDoctorResultsTTY([]app.DoctorResult{
		{
			ID:      "github-api",
			Status:  app.DoctorStatusFail,
			Message: "GitHub API check failed: boom",
		},
		{
			ID:      "bin-dir-on-path",
			Status:  app.DoctorStatusWarn,
			Message: "managed bin directory /tmp/bin is not on PATH",
		},
		{
			ID:      "trusted-root",
			Status:  app.DoctorStatusPass,
			Message: "using built-in Sigstore trust roots",
		},
	}, false)

	assert.Contains(t, got, "doctor")
	assert.Contains(t, got, "fail")
	assert.Contains(t, got, "warn")
	assert.Contains(t, got, "pass")
	assert.Contains(t, got, "github-api  GitHub API check failed: boom")
	assert.Contains(t, got, "Warn: 1")
	assert.Contains(t, got, "Pass: 1")
}
