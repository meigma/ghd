package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

func TestWritePackageListJSONUsesStableStringTargets(t *testing.T) {
	var buf bytes.Buffer

	err := writePackageListJSON(Options{Out: &buf}, []app.PackageListResult{
		{
			Repository:  verification.Repository{Owner: "owner", Name: "repo"},
			PackageName: "foo",
			Binaries:    []string{"foo"},
		},
	})

	require.NoError(t, err)
	var got packageListJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got.Packages, 1)
	assert.Equal(t, "owner/repo", got.Packages[0].Repository)
	assert.Equal(t, "foo", got.Packages[0].Package)
	assert.Equal(t, "owner/repo/foo", got.Packages[0].Target)
	assert.Equal(t, []string{"foo"}, got.Packages[0].Binaries)
}

func TestWritePackageInfoJSONNormalizesEmptySlices(t *testing.T) {
	var buf bytes.Buffer

	err := writePackageInfoJSON(Options{Out: &buf}, app.PackageInfoResult{
		Repository:     verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:    "foo",
		SignerWorkflow: verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"),
		TagPattern:     "v${version}",
	})

	require.NoError(t, err)
	var got packageInfoJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "owner/repo/foo", got.Package.Target)
	assert.Equal(t, "owner/repo/.github/workflows/release.yml", got.Package.SignerWorkflow)
	assert.NotNil(t, got.Package.Binaries)
	assert.NotNil(t, got.Package.Assets)
}

func TestWriteInstalledListJSONIncludesStoreMetadata(t *testing.T) {
	installedAt := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer

	err := writeInstalledListJSON(Options{Out: &buf}, []state.Record{
		{
			Repository:       "owner/repo",
			Package:          "foo",
			Version:          "1.2.3",
			Tag:              "v1.2.3",
			Asset:            "foo.tar.gz",
			AssetDigest:      "sha256:abc123",
			StorePath:        "/store/foo",
			ArtifactPath:     "/store/foo/artifact",
			ExtractedPath:    "/store/foo/extracted",
			VerificationPath: "/store/foo/verification.json",
			InstalledAt:      installedAt,
			Binaries: []state.Binary{
				{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo/extracted/foo"},
			},
		},
	})

	require.NoError(t, err)
	var got installedListJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got.Installed, 1)
	assert.Equal(t, "owner/repo/foo", got.Installed[0].Target)
	assert.Equal(t, "/store/foo/verification.json", got.Installed[0].VerificationPath)
	assert.Equal(t, installedAt, got.Installed[0].InstalledAt)
	assert.Equal(t, []installedBinaryJSON{
		{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo/extracted/foo"},
	}, got.Installed[0].Binaries)
}

func TestWriteRowResultJSONShapes(t *testing.T) {
	tests := []struct {
		name   string
		write  func(Options) error
		assert func(t *testing.T, data []byte)
	}{
		{
			name: "check results",
			write: func(options Options) error {
				return writeCheckResultsJSON(options, []app.CheckResult{
					{
						Repository:    "owner/repo",
						Package:       "foo",
						Version:       "1.2.3",
						Status:        app.CheckStatusUpdateAvailable,
						LatestVersion: "1.3.0",
					},
				})
			},
			assert: func(t *testing.T, data []byte) {
				var got checkResultsJSON
				require.NoError(t, json.Unmarshal(data, &got))
				require.Len(t, got.Checks, 1)
				assert.Equal(t, "owner/repo/foo", got.Checks[0].Target)
				assert.Equal(t, "update-available", got.Checks[0].Status)
				assert.Equal(t, "1.3.0", got.Checks[0].LatestVersion)
			},
		},
		{
			name: "verify results",
			write: func(options Options) error {
				return writeVerifyResultsJSON(options, []app.VerifyInstalledResult{
					{
						Repository: "owner/repo",
						Package:    "foo",
						Version:    "1.2.3",
						Status:     app.VerifyStatusCannotVerify,
						Reason:     "tampered",
					},
				})
			},
			assert: func(t *testing.T, data []byte) {
				var got verifyResultsJSON
				require.NoError(t, json.Unmarshal(data, &got))
				require.Len(t, got.Verifications, 1)
				assert.Equal(t, "owner/repo/foo", got.Verifications[0].Target)
				assert.Equal(t, "cannot-verify", got.Verifications[0].Status)
				assert.Equal(t, "tampered", got.Verifications[0].Reason)
			},
		},
		{
			name: "update results",
			write: func(options Options) error {
				return writeUpdateResultsJSON(options, []app.UpdateInstalledResult{
					{
						Repository:      "owner/repo",
						Package:         "foo",
						PreviousVersion: "1.2.3",
						CurrentVersion:  "1.3.0",
						Status:          app.UpdateStatusUpdated,
					},
				})
			},
			assert: func(t *testing.T, data []byte) {
				var got updateResultsJSON
				require.NoError(t, json.Unmarshal(data, &got))
				require.Len(t, got.Updates, 1)
				assert.Equal(t, "owner/repo/foo", got.Updates[0].Target)
				assert.Equal(t, "updated", got.Updates[0].Status)
				assert.Equal(t, "1.2.3", got.Updates[0].PreviousVersion)
				assert.Equal(t, "1.3.0", got.Updates[0].CurrentVersion)
			},
		},
		{
			name: "doctor results",
			write: func(options Options) error {
				return writeDoctorResultsJSON(options, []app.DoctorResult{
					{ID: "github-api", Status: app.DoctorStatusWarn, Message: "token missing"},
				})
			},
			assert: func(t *testing.T, data []byte) {
				var got doctorResultsJSON
				require.NoError(t, json.Unmarshal(data, &got))
				require.Len(t, got.Checks, 1)
				assert.Equal(t, "github-api", got.Checks[0].ID)
				assert.Equal(t, "warn", got.Checks[0].Status)
				assert.Equal(t, "token missing", got.Checks[0].Message)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			err := tt.write(Options{Out: &buf})

			require.NoError(t, err)
			tt.assert(t, buf.Bytes())
		})
	}
}

func TestWriteRepositoryListJSONUsesStringRepositories(t *testing.T) {
	refreshedAt := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer

	err := writeRepositoryListJSON(Options{Out: &buf}, []catalog.RepositoryRecord{
		{
			Repository: verification.Repository{Owner: "owner", Name: "repo"},
			Packages: []catalog.PackageSummary{
				{Name: "foo", Description: "Foo CLI"},
				{Name: "bar", Binaries: []string{"bar"}},
			},
			RefreshedAt: refreshedAt,
		},
	})

	require.NoError(t, err)
	var got repositoryListJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got.Repositories, 1)
	assert.Equal(t, "owner/repo", got.Repositories[0].Repository)
	assert.Equal(t, refreshedAt, got.Repositories[0].RefreshedAt)
	require.Len(t, got.Repositories[0].Packages, 2)
	assert.Equal(t, "Foo CLI", got.Repositories[0].Packages[0].Description)
	assert.NotNil(t, got.Repositories[0].Packages[0].Binaries)
	assert.Equal(t, []string{"bar"}, got.Repositories[0].Packages[1].Binaries)
}
