package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/verification"
)

func TestInstallerCreatesDigestKeyedStoreLayout(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "artifact.tar.gz")
	require.NoError(t, os.WriteFile(artifact, []byte("artifact"), 0o600))
	digest, err := verification.NewDigest("sha256", repeatHexForFilesystem("aa", 32))
	require.NoError(t, err)
	storeRoot := t.TempDir()

	layout, err := NewInstaller().CreateStoreLayout(context.Background(), app.StoreLayoutRequest{
		StoreRoot:    storeRoot,
		Repository:   verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:  "foo",
		Version:      "1.2.3",
		AssetDigest:  digest,
		ArtifactPath: artifact,
	})

	require.NoError(t, err)
	wantStorePath := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-"+digest.Hex)
	assert.Equal(t, wantStorePath, layout.StorePath)
	assert.Equal(t, filepath.Join(wantStorePath, "artifact"), layout.ArtifactPath)
	assert.Equal(t, filepath.Join(wantStorePath, "extracted"), layout.ExtractedDir)
	data, err := os.ReadFile(layout.ArtifactPath)
	require.NoError(t, err)
	assert.Equal(t, "artifact", string(data))
}

func TestInstallerRequiresFreshExtractionDirectory(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "artifact.tar.gz")
	require.NoError(t, os.WriteFile(artifact, []byte("artifact"), 0o600))
	digest, err := verification.NewDigest("sha256", repeatHexForFilesystem("aa", 32))
	require.NoError(t, err)
	storeRoot := t.TempDir()
	storePath := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-"+digest.Hex)
	staleExtracted := filepath.Join(storePath, "extracted")
	require.NoError(t, os.MkdirAll(staleExtracted, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staleExtracted, "foo"), []byte("stale"), 0o755))

	_, err = NewInstaller().CreateStoreLayout(context.Background(), app.StoreLayoutRequest{
		StoreRoot:    storeRoot,
		Repository:   verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:  "foo",
		Version:      "1.2.3",
		AssetDigest:  digest,
		ArtifactPath: artifact,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.NoFileExists(t, filepath.Join(storePath, "artifact"))
}

func TestInstallerRemovesStoreLayout(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store")
	require.NoError(t, os.MkdirAll(filepath.Join(storePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storePath, "artifact"), []byte("artifact"), 0o600))

	err := NewInstaller().RemoveStoreLayout(context.Background(), app.StoreLayout{
		StorePath: storePath,
	})

	require.NoError(t, err)
	assert.NoDirExists(t, storePath)
}

func TestInstallerLinksBinariesAndFailsClosedOnCollision(t *testing.T) {
	installer := NewInstaller()
	binDir := t.TempDir()
	targetDir := t.TempDir()
	fooTarget := filepath.Join(targetDir, "foo")
	barTarget := filepath.Join(targetDir, "bar")
	require.NoError(t, os.WriteFile(fooTarget, []byte("foo"), 0o755))
	require.NoError(t, os.WriteFile(barTarget, []byte("bar"), 0o755))

	links, err := installer.LinkBinaries(context.Background(), app.LinkBinariesRequest{
		BinDir: binDir,
		Binaries: []app.ExtractedBinary{
			{Name: "foo", Path: fooTarget},
		},
	})

	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, filepath.Join(binDir, "foo"), links[0].LinkPath)
	gotTarget, err := os.Readlink(links[0].LinkPath)
	require.NoError(t, err)
	assert.Equal(t, fooTarget, gotTarget)

	existingBar := filepath.Join(binDir, "bar")
	require.NoError(t, os.WriteFile(existingBar, []byte("existing"), 0o644))
	_, err = installer.LinkBinaries(context.Background(), app.LinkBinariesRequest{
		BinDir: binDir,
		Binaries: []app.ExtractedBinary{
			{Name: "baz", Path: fooTarget},
			{Name: "bar", Path: barTarget},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.NoFileExists(t, filepath.Join(binDir, "baz"))
	data, err := os.ReadFile(existingBar)
	require.NoError(t, err)
	assert.Equal(t, "existing", string(data))
}

func TestInstallerRemovesOnlyExpectedBinaryLinks(t *testing.T) {
	installer := NewInstaller()
	binDir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "foo")
	require.NoError(t, os.WriteFile(target, []byte("foo"), 0o755))
	linkPath := filepath.Join(binDir, "foo")
	require.NoError(t, os.Symlink(target, linkPath))

	err := installer.RemoveBinaryLinks(context.Background(), []app.InstalledBinary{
		{Name: "foo", LinkPath: linkPath, TargetPath: target},
	})

	require.NoError(t, err)
	assert.NoFileExists(t, linkPath)

	unsafePath := filepath.Join(binDir, "unsafe")
	require.NoError(t, os.WriteFile(unsafePath, []byte("not a symlink"), 0o644))
	err = installer.RemoveBinaryLinks(context.Background(), []app.InstalledBinary{
		{Name: "unsafe", LinkPath: unsafePath, TargetPath: target},
	})

	require.Error(t, err)
	assert.FileExists(t, unsafePath)

	wrongTarget := filepath.Join(targetDir, "wrong")
	require.NoError(t, os.WriteFile(wrongTarget, []byte("wrong"), 0o755))
	swappedPath := filepath.Join(binDir, "swapped")
	require.NoError(t, os.Symlink(wrongTarget, swappedPath))
	err = installer.RemoveBinaryLinks(context.Background(), []app.InstalledBinary{
		{Name: "swapped", LinkPath: swappedPath, TargetPath: target},
	})

	require.Error(t, err)
	assert.FileExists(t, swappedPath)
}

func TestInstallerWritesInstallMetadata(t *testing.T) {
	storePath := t.TempDir()
	path, err := NewInstaller().WriteInstallMetadata(context.Background(), storePath, app.InstallRecord{
		SchemaVersion: 1,
		Repository:    "owner/repo",
		Package:       "foo",
		Version:       "1.2.3",
		Asset:         "foo.tar.gz",
		Binaries: []app.InstalledBinary{
			{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(storePath, "install.json"), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var record app.InstallRecord
	require.NoError(t, json.Unmarshal(data, &record))
	assert.Equal(t, "owner/repo", record.Repository)
	require.Len(t, record.Binaries, 1)
	assert.Equal(t, "/bin/foo", record.Binaries[0].LinkPath)
}

func repeatHexForFilesystem(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}
