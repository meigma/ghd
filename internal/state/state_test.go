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
