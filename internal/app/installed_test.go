package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/state"
)

func TestInstalledPackagesListInstalledReturnsActiveRecords(t *testing.T) {
	store := &fakeInstalledStateStore{index: state.NewIndex()}
	var err error
	store.index, err = store.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	subject, err := NewInstalledPackages(InstalledPackagesDependencies{StateStore: store})
	require.NoError(t, err)

	result, err := subject.ListInstalled(context.Background(), filepath.Join(t.TempDir(), "state"))

	require.NoError(t, err)
	assert.Equal(t, store.index.Records, result)
}

func TestInstalledPackagesListInstalledRequiresStateDir(t *testing.T) {
	subject, err := NewInstalledPackages(InstalledPackagesDependencies{StateStore: &fakeInstalledStateStore{}})
	require.NoError(t, err)

	_, err = subject.ListInstalled(context.Background(), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "state directory")
}

func installedRecord(repository string, packageName string) state.Record {
	return state.Record{
		Repository:       repository,
		Package:          packageName,
		Version:          "1.2.3",
		Tag:              "v1.2.3",
		Asset:            packageName + "_1.2.3_darwin_arm64.tar.gz",
		AssetDigest:      "sha256:abc123",
		StorePath:        "/store/foo",
		ArtifactPath:     "/store/foo/artifact",
		ExtractedPath:    "/store/foo/extracted",
		VerificationPath: "/store/foo/verification.json",
		Binaries:         []state.Binary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo/extracted/foo"}},
		InstalledAt:      time.Unix(1700000000, 0).UTC(),
	}
}
