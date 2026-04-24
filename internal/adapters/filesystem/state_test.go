package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/state"
)

func TestInstalledStoreLoadMissingStateReturnsEmptyIndex(t *testing.T) {
	store := NewInstalledStore()

	index, err := store.LoadInstalledState(context.Background(), filepath.Join(t.TempDir(), "state"))

	require.NoError(t, err)
	assert.Equal(t, 1, index.SchemaVersion)
	assert.Empty(t, index.Records)
}

func TestInstalledStoreRoundTripsInstalledState(t *testing.T) {
	store := NewInstalledStore()
	stateDir := t.TempDir()
	index, err := store.AddInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/repo", "foo"))
	require.NoError(t, err)
	loaded, err := store.LoadInstalledState(context.Background(), stateDir)

	require.NoError(t, err)
	assert.Equal(t, index.Normalize(), loaded)
	assert.FileExists(t, filepath.Join(stateDir, "installed.json"))
}

func TestInstalledStoreAddInstalledRecordMergesConcurrentWriters(t *testing.T) {
	store := NewInstalledStore()
	stateDir := t.TempDir()
	const installs = 8
	var wg sync.WaitGroup
	errs := make(chan error, installs)

	for i := range installs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			record := installedStateRecord("owner/repo", fmt.Sprintf("pkg-%d", i))
			record.Binaries[0].Name = record.Package
			errs <- func() error {
				_, err := store.AddInstalledRecord(context.Background(), stateDir, record)
				return err
			}()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	loaded, err := store.LoadInstalledState(context.Background(), stateDir)

	require.NoError(t, err)
	assert.Len(t, loaded.Records, installs)
}

func TestInstalledStoreAddInstalledRecordIgnoresStaleLockFile(t *testing.T) {
	store := NewInstalledStore()
	stateDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, ".installed.lock"), []byte("stale"), 0o600))

	_, err := store.AddInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/repo", "foo"))

	require.NoError(t, err)
	loaded, err := store.LoadInstalledState(context.Background(), stateDir)
	require.NoError(t, err)
	assert.Len(t, loaded.Records, 1)
}

func TestInstalledStoreRemoveInstalledRecord(t *testing.T) {
	store := NewInstalledStore()
	stateDir := t.TempDir()
	_, err := store.AddInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/repo", "foo"))
	require.NoError(t, err)
	bar := installedStateRecord("owner/repo", "bar")
	bar.Binaries[0].Name = "bar"
	bar.Binaries[0].LinkPath = "/bin/bar"
	bar.Binaries[0].TargetPath = "/store/foo/extracted/bar"
	_, err = store.AddInstalledRecord(context.Background(), stateDir, bar)
	require.NoError(t, err)

	index, err := store.RemoveInstalledRecord(context.Background(), stateDir, "owner/repo", "foo")

	require.NoError(t, err)
	assert.Len(t, index.Records, 1)
	assert.Equal(t, "bar", index.Records[0].Package)
	loaded, err := store.LoadInstalledState(context.Background(), stateDir)
	require.NoError(t, err)
	assert.Equal(t, index, loaded)

	_, err = store.RemoveInstalledRecord(context.Background(), stateDir, "owner/repo", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestInstalledStoreAddInstalledRecordRejectsBinaryOwnershipCollision(t *testing.T) {
	store := NewInstalledStore()
	stateDir := t.TempDir()
	_, err := store.AddInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/repo", "foo"))
	require.NoError(t, err)

	_, err = store.AddInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/other", "bar"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `binary "foo" is already owned by owner/repo/foo`)
	loaded, loadErr := store.LoadInstalledState(context.Background(), stateDir)
	require.NoError(t, loadErr)
	require.Len(t, loaded.Records, 1)
	assert.Equal(t, "foo", loaded.Records[0].Package)
}

func TestInstalledStoreReplaceInstalledRecord(t *testing.T) {
	store := NewInstalledStore()
	stateDir := t.TempDir()
	_, err := store.AddInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/repo", "foo"))
	require.NoError(t, err)

	replacement := installedStateRecord("owner/repo", "foo")
	replacement.Version = "1.3.0"
	replacement.Tag = "v1.3.0"
	replacement.Asset = "foo_1.3.0_darwin_arm64.tar.gz"
	replacement.StorePath = "/store/foo-new"
	replacement.ArtifactPath = "/store/foo-new/artifact"
	replacement.ExtractedPath = "/store/foo-new/extracted"
	replacement.VerificationPath = "/store/foo-new/verification.json"
	index, err := store.ReplaceInstalledRecord(context.Background(), stateDir, replacement)

	require.NoError(t, err)
	require.Len(t, index.Records, 1)
	assert.Equal(t, "1.3.0", index.Records[0].Version)
	assert.Equal(t, "/store/foo-new", index.Records[0].StorePath)

	_, err = store.ReplaceInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/repo", "missing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestInstalledStoreReplaceInstalledRecordRejectsBinaryOwnershipCollision(t *testing.T) {
	store := NewInstalledStore()
	stateDir := t.TempDir()
	_, err := store.AddInstalledRecord(context.Background(), stateDir, installedStateRecord("owner/repo", "foo"))
	require.NoError(t, err)
	bar := installedStateRecord("owner/other", "bar")
	bar.Binaries[0].Name = "bar"
	bar.Binaries[0].LinkPath = "/bin/bar"
	bar.Binaries[0].TargetPath = "/store/bar/extracted/bar"
	_, err = store.AddInstalledRecord(context.Background(), stateDir, bar)
	require.NoError(t, err)

	replacement := installedStateRecord("owner/repo", "foo")
	replacement.Binaries[0].Name = "bar"
	replacement.Binaries[0].LinkPath = "/other-bin/bar"
	replacement.Binaries[0].TargetPath = "/store/foo/extracted/bar"
	_, err = store.ReplaceInstalledRecord(context.Background(), stateDir, replacement)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `binary "bar" is already owned by owner/other/bar`)
	loaded, loadErr := store.LoadInstalledState(context.Background(), stateDir)
	require.NoError(t, loadErr)
	require.Len(t, loaded.Records, 2)
	record, ok := loaded.Record("owner/repo", "foo")
	require.True(t, ok)
	assert.Equal(t, "foo", record.Binaries[0].Name)
}

func TestInstalledStoreRejectsMalformedState(t *testing.T) {
	stateDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "installed.json"), []byte("{"), 0o644))

	_, err := NewInstalledStore().LoadInstalledState(context.Background(), stateDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode installed state")
}

func TestInstalledStoreRejectsMissingSchemaVersion(t *testing.T) {
	stateDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "installed.json"), []byte("{}\n"), 0o644))

	_, err := NewInstalledStore().LoadInstalledState(context.Background(), stateDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported installed state version 0")
}

func installedStateRecord(repository string, packageName string) state.Record {
	return state.Record{
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
		Binaries:         []state.Binary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo/extracted/foo"}},
		InstalledAt:      time.Unix(1700000000, 0).UTC(),
	}
}
