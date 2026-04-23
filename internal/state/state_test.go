package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexAddRecordRejectsDuplicateActiveInstall(t *testing.T) {
	index := NewIndex()
	record := installedRecord("owner/repo", "foo")

	index, err := index.AddRecord(record)
	require.NoError(t, err)

	_, err = index.AddRecord(record)

	require.Error(t, err)
	var duplicate DuplicateInstallError
	require.ErrorAs(t, err, &duplicate)
	assert.Equal(t, "owner/repo", duplicate.Repository)
	assert.Equal(t, "foo", duplicate.Package)
}

func TestIndexResolveTarget(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	index, err = index.AddRecord(withBinaries(installedRecord("owner/other", "bar"), []Binary{
		{Name: "baz", LinkPath: "/bin/baz", TargetPath: "/store/bar/extracted/baz"},
	}))
	require.NoError(t, err)

	tests := []struct {
		name        string
		target      string
		wantRepo    string
		wantPackage string
	}{
		{
			name:        "package name",
			target:      "foo",
			wantRepo:    "owner/repo",
			wantPackage: "foo",
		},
		{
			name:        "binary name",
			target:      "baz",
			wantRepo:    "owner/other",
			wantPackage: "bar",
		},
		{
			name:        "qualified package",
			target:      "owner/repo/foo",
			wantRepo:    "owner/repo",
			wantPackage: "foo",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			record, err := index.ResolveTarget(tt.target)

			require.NoError(t, err)
			assert.Equal(t, tt.wantRepo, record.Repository)
			assert.Equal(t, tt.wantPackage, record.Package)
		})
	}
}

func TestIndexResolveTargetReportsAmbiguousAndMissingInstalls(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.AddRecord(installedRecord("owner/one", "foo"))
	require.NoError(t, err)
	index, err = index.AddRecord(withBinaries(installedRecord("owner/two", "bar"), []Binary{
		{Name: "foo", LinkPath: "/bin/foo-two", TargetPath: "/store/bar/extracted/foo"},
	}))
	require.NoError(t, err)

	_, err = index.ResolveTarget("foo")
	require.Error(t, err)
	var ambiguous AmbiguousInstallError
	require.ErrorAs(t, err, &ambiguous)
	assert.Equal(t, "foo", ambiguous.Target)
	require.Len(t, ambiguous.Matches, 2)

	_, err = index.ResolveTarget("missing")
	require.Error(t, err)
	var notInstalled NotInstalledError
	require.ErrorAs(t, err, &notInstalled)
	assert.Equal(t, "missing", notInstalled.Target)
}

func TestIndexRemoveRecordRemovesActiveInstall(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	index, err = index.AddRecord(installedRecord("owner/other", "bar"))
	require.NoError(t, err)

	next, removed, err := index.RemoveRecord("owner/repo", "foo")

	require.NoError(t, err)
	assert.Equal(t, "owner/repo", removed.Repository)
	assert.Equal(t, "foo", removed.Package)
	_, ok := next.Record("owner/repo", "foo")
	assert.False(t, ok)
	_, ok = next.Record("owner/other", "bar")
	assert.True(t, ok)

	_, _, err = next.RemoveRecord("owner/repo", "foo")
	require.Error(t, err)
	var notInstalled NotInstalledError
	require.ErrorAs(t, err, &notInstalled)
}

func TestIndexReplaceRecordReplacesActiveInstall(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)

	replacement := installedRecord("owner/repo", "foo")
	replacement.Version = "1.3.0"
	replacement.Tag = "v1.3.0"
	replacement.Asset = "foo_1.3.0_darwin_arm64.tar.gz"
	replacement.StorePath = "/store/foo-new"
	replacement.ArtifactPath = "/store/foo-new/artifact"
	replacement.ExtractedPath = "/store/foo-new/extracted"
	replacement.VerificationPath = "/store/foo-new/verification.json"

	index, err = index.ReplaceRecord(replacement)

	require.NoError(t, err)
	record, ok := index.Record("owner/repo", "foo")
	require.True(t, ok)
	assert.Equal(t, "1.3.0", record.Version)
	assert.Equal(t, "/store/foo-new", record.StorePath)

	_, err = index.ReplaceRecord(installedRecord("owner/repo", "missing"))
	require.Error(t, err)
	var notInstalled NotInstalledError
	require.ErrorAs(t, err, &notInstalled)
	assert.Equal(t, "owner/repo/missing", notInstalled.Target)
}

func TestIndexNormalizeSortsRecordsAndBinaries(t *testing.T) {
	index := Index{
		Records: []Record{
			withBinaries(installedRecord("owner/zeta", "beta"), []Binary{
				{Name: "zeta", LinkPath: "/bin/zeta", TargetPath: "/store/zeta"},
				{Name: "alpha", LinkPath: "/bin/alpha", TargetPath: "/store/alpha"},
			}),
			installedRecord("owner/alpha", "gamma"),
		},
	}

	normalized := index.Normalize()

	assert.Equal(t, schemaVersion, normalized.SchemaVersion)
	require.Len(t, normalized.Records, 2)
	assert.Equal(t, "owner/alpha", normalized.Records[0].Repository)
	assert.Equal(t, "owner/zeta", normalized.Records[1].Repository)
	assert.Equal(t, []Binary{
		{Name: "alpha", LinkPath: "/bin/alpha", TargetPath: "/store/alpha"},
		{Name: "zeta", LinkPath: "/bin/zeta", TargetPath: "/store/zeta"},
	}, normalized.Records[1].Binaries)
}

func TestIndexValidateRejectsMalformedRecords(t *testing.T) {
	tests := []struct {
		name  string
		index Index
		want  string
	}{
		{
			name:  "unsupported schema",
			index: Index{SchemaVersion: 99},
			want:  "unsupported installed state version",
		},
		{
			name:  "missing repository",
			index: Index{SchemaVersion: schemaVersion, Records: []Record{{Package: "foo"}}},
			want:  "installed repository must be set",
		},
		{
			name:  "duplicate package",
			index: Index{SchemaVersion: schemaVersion, Records: []Record{installedRecord("owner/repo", "foo"), installedRecord("OWNER/repo", "foo")}},
			want:  "already installed",
		},
		{
			name:  "missing binary target",
			index: Index{SchemaVersion: schemaVersion, Records: []Record{withBinaries(installedRecord("owner/repo", "foo"), []Binary{{Name: "foo", LinkPath: "/bin/foo"}})}},
			want:  "target path",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.index.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func installedRecord(repository string, packageName string) Record {
	return Record{
		Repository:       repository,
		Package:          packageName,
		Version:          "1.2.3",
		Tag:              "v1.2.3",
		Asset:            "foo.tar.gz",
		AssetDigest:      "sha256:abc123",
		StorePath:        "/store/foo",
		ArtifactPath:     "/store/foo/artifact",
		ExtractedPath:    "/store/foo/extracted",
		VerificationPath: "/store/foo/verification.json",
		Binaries:         []Binary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo/extracted/foo"}},
		InstalledAt:      time.Unix(1700000000, 0).UTC(),
	}
}

func withBinaries(record Record, binaries []Binary) Record {
	record.Binaries = binaries
	return record
}
