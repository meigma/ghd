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

func TestInstallerPublishesVerifiedArtifactExclusively(t *testing.T) {
	source := filepath.Join(t.TempDir(), "artifact.tar.gz")
	require.NoError(t, os.WriteFile(source, []byte("verified"), 0o600))
	outputDir := t.TempDir()

	path, err := NewInstaller().PublishVerifiedArtifact(context.Background(), source, outputDir, "foo.tar.gz")

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(outputDir, "foo.tar.gz"), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "verified", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestInstallerPublishVerifiedArtifactFailsIfDestinationExists(t *testing.T) {
	source := filepath.Join(t.TempDir(), "artifact.tar.gz")
	require.NoError(t, os.WriteFile(source, []byte("verified"), 0o600))
	outputDir := t.TempDir()
	destination := filepath.Join(outputDir, "foo.tar.gz")
	require.NoError(t, os.WriteFile(destination, []byte("original"), 0o600))

	_, err := NewInstaller().PublishVerifiedArtifact(context.Background(), source, outputDir, "foo.tar.gz")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
}

func TestInstallerPublishVerifiedArtifactRejectsUnsafeNames(t *testing.T) {
	source := filepath.Join(t.TempDir(), "artifact.tar.gz")
	require.NoError(t, os.WriteFile(source, []byte("verified"), 0o600))

	tests := []struct {
		name      string
		assetName string
	}{
		{name: "empty", assetName: ""},
		{name: "parent", assetName: "../foo.tar.gz"},
		{name: "backslash", assetName: `foo\bar.tar.gz`},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewInstaller().PublishVerifiedArtifact(context.Background(), source, t.TempDir(), tt.assetName)

			require.Error(t, err)
		})
	}
}

func TestInstallerPublishVerifiedArtifactRemovesPartialCopy(t *testing.T) {
	sourceDir := t.TempDir()
	outputDir := t.TempDir()
	destination := filepath.Join(outputDir, "foo.tar.gz")

	_, err := NewInstaller().PublishVerifiedArtifact(context.Background(), sourceDir, outputDir, "foo.tar.gz")

	require.Error(t, err)
	assert.NoFileExists(t, destination)
}

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

func TestInstallerCreatesAbsoluteStoreLayoutFromRelativeStoreRoot(t *testing.T) {
	oldwd, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldwd))
	})
	artifact := filepath.Join(t.TempDir(), "artifact.tar.gz")
	require.NoError(t, os.WriteFile(artifact, []byte("artifact"), 0o600))
	digest, err := verification.NewDigest("sha256", repeatHexForFilesystem("aa", 32))
	require.NoError(t, err)
	storeRoot, err := filepath.Abs("store")
	require.NoError(t, err)

	layout, err := NewInstaller().CreateStoreLayout(context.Background(), app.StoreLayoutRequest{
		StoreRoot:    "store",
		Repository:   verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:  "foo",
		Version:      "1.2.3",
		AssetDigest:  digest,
		ArtifactPath: artifact,
	})

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-"+digest.Hex), layout.StorePath)
	assert.FileExists(t, layout.ArtifactPath)
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

func TestInstallerRemoveManagedInstallRemovesStorePathUnderRoot(t *testing.T) {
	storeRoot := t.TempDir()
	storePath := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-abc123")
	require.NoError(t, os.MkdirAll(filepath.Join(storePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storePath, "artifact"), []byte("artifact"), 0o600))
	untouched := filepath.Join(storeRoot, "github.com", "owner", "repo", "bar")
	require.NoError(t, os.MkdirAll(untouched, 0o755))

	err := NewInstaller().RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
		StoreRoot: storeRoot,
		StorePath: storePath,
	})

	require.NoError(t, err)
	assert.NoDirExists(t, storePath)
	assert.DirExists(t, untouched)
}

func TestInstallerRemoveManagedInstallRejectsUnsafeStorePaths(t *testing.T) {
	storeRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))

	tests := []struct {
		name      string
		storeRoot string
		storePath string
		want      string
	}{
		{
			name:      "empty root",
			storePath: filepath.Join(storeRoot, "pkg"),
			want:      "store root must be set",
		},
		{
			name:      "empty path",
			storeRoot: storeRoot,
			want:      "store path must be set",
		},
		{
			name:      "relative path",
			storeRoot: storeRoot,
			storePath: "store/github.com/owner/repo/foo",
			want:      "must be absolute",
		},
		{
			name:      "root path",
			storeRoot: storeRoot,
			storePath: storeRoot,
			want:      "not under store root",
		},
		{
			name:      "outside root",
			storeRoot: storeRoot,
			storePath: outside,
			want:      "not under store root",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := NewInstaller().RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
				StoreRoot: tt.storeRoot,
				StorePath: tt.storePath,
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
	assert.DirExists(t, outside)
}

func TestInstallerRemoveManagedInstallRejectsSymlinkedStoreComponents(t *testing.T) {
	storeRoot := t.TempDir()
	outside := t.TempDir()
	outsideStorePath := filepath.Join(outside, "repo", "foo")
	require.NoError(t, os.MkdirAll(outsideStorePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outsideStorePath, "keep"), []byte("outside"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(storeRoot, "github.com"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(storeRoot, "github.com", "owner")))

	err := NewInstaller().RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
		StoreRoot: storeRoot,
		StorePath: filepath.Join(storeRoot, "github.com", "owner", "repo", "foo"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink component")
	assert.FileExists(t, filepath.Join(outsideStorePath, "keep"))
}

func TestInstallerCreateStoreLayoutRejectsSymlinkedStoreComponents(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "artifact.tar.gz")
	require.NoError(t, os.WriteFile(artifact, []byte("artifact"), 0o600))
	digest, err := verification.NewDigest("sha256", repeatHexForFilesystem("aa", 32))
	require.NoError(t, err)
	storeRoot := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(storeRoot, "github.com"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(storeRoot, "github.com", "owner")))

	_, err = NewInstaller().CreateStoreLayout(context.Background(), app.StoreLayoutRequest{
		StoreRoot:    storeRoot,
		Repository:   verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:  "foo",
		Version:      "1.2.3",
		AssetDigest:  digest,
		ArtifactPath: artifact,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink component")
	assert.NoDirExists(t, filepath.Join(outside, "repo"))
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
		Binaries: []app.MaterializedBinary{
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
		Binaries: []app.MaterializedBinary{
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

func TestInstallerLinksBinariesWithAbsolutePathsFromRelativeBinRoot(t *testing.T) {
	installer := NewInstaller()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	workDir := t.TempDir()
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalDir))
	})
	target := filepath.Join(t.TempDir(), "foo")
	require.NoError(t, os.WriteFile(target, []byte("foo"), 0o755))
	wantLinkPath, err := filepath.Abs(filepath.Join("bin", "foo"))
	require.NoError(t, err)

	links, err := installer.LinkBinaries(context.Background(), app.LinkBinariesRequest{
		BinDir: "bin",
		Binaries: []app.MaterializedBinary{
			{Name: "foo", Path: target},
		},
	})

	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, wantLinkPath, links[0].LinkPath)
}

func TestInstallerReplaceManagedBinariesSwapsActiveLinks(t *testing.T) {
	installer := NewInstaller()
	binDir := t.TempDir()
	oldTarget := filepath.Join(t.TempDir(), "old")
	newTarget := filepath.Join(t.TempDir(), "new")
	require.NoError(t, os.WriteFile(oldTarget, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(newTarget, []byte("new"), 0o755))
	linkPath := filepath.Join(binDir, "foo")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.Symlink(oldTarget, linkPath))

	err := installer.ReplaceManagedBinaries(context.Background(), app.ReplaceManagedBinariesRequest{
		BinDir: binDir,
		Previous: []app.InstalledBinary{
			{Name: "foo", LinkPath: linkPath, TargetPath: oldTarget},
		},
		Next: []app.InstalledBinary{
			{Name: "foo", LinkPath: linkPath, TargetPath: newTarget},
		},
	})

	require.NoError(t, err)
	gotTarget, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, newTarget, gotTarget)
}

func TestInstallerReplaceManagedBinariesRestoresPreviousLinksWhenNewLinksFail(t *testing.T) {
	installer := NewInstaller()
	binDir := t.TempDir()
	oldTarget := filepath.Join(t.TempDir(), "old")
	require.NoError(t, os.WriteFile(oldTarget, []byte("old"), 0o755))
	linkPath := filepath.Join(binDir, "foo")
	blockingPath := filepath.Join(binDir, "bar")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.Symlink(oldTarget, linkPath))
	require.NoError(t, os.WriteFile(blockingPath, []byte("existing"), 0o644))
	newFooTarget := filepath.Join(t.TempDir(), "new-foo")
	newBarTarget := filepath.Join(t.TempDir(), "new-bar")
	require.NoError(t, os.WriteFile(newFooTarget, []byte("new foo"), 0o755))
	require.NoError(t, os.WriteFile(newBarTarget, []byte("new bar"), 0o755))

	err := installer.ReplaceManagedBinaries(context.Background(), app.ReplaceManagedBinariesRequest{
		BinDir: binDir,
		Previous: []app.InstalledBinary{
			{Name: "foo", LinkPath: linkPath, TargetPath: oldTarget},
		},
		Next: []app.InstalledBinary{
			{Name: "foo", LinkPath: linkPath, TargetPath: newFooTarget},
			{Name: "bar", LinkPath: blockingPath, TargetPath: newBarTarget},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	gotTarget, readErr := os.Readlink(linkPath)
	require.NoError(t, readErr)
	assert.Equal(t, oldTarget, gotTarget)
	data, readErr := os.ReadFile(blockingPath)
	require.NoError(t, readErr)
	assert.Equal(t, "existing", string(data))
}

func TestInstallerReplaceManagedBinariesRejectsNewLinksOutsideBinRoot(t *testing.T) {
	installer := NewInstaller()
	binDir := t.TempDir()
	oldTarget := filepath.Join(t.TempDir(), "old")
	newTarget := filepath.Join(t.TempDir(), "new")
	require.NoError(t, os.WriteFile(oldTarget, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(newTarget, []byte("new"), 0o755))
	linkPath := filepath.Join(binDir, "foo")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.Symlink(oldTarget, linkPath))
	outsideLink := filepath.Join(t.TempDir(), "foo")

	err := installer.ReplaceManagedBinaries(context.Background(), app.ReplaceManagedBinariesRequest{
		BinDir: binDir,
		Previous: []app.InstalledBinary{
			{Name: "foo", LinkPath: linkPath, TargetPath: oldTarget},
		},
		Next: []app.InstalledBinary{
			{Name: "foo", LinkPath: outsideLink, TargetPath: newTarget},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not under bin root")
	gotTarget, readErr := os.Readlink(linkPath)
	require.NoError(t, readErr)
	assert.Equal(t, oldTarget, gotTarget)
	assert.NoFileExists(t, outsideLink)
}

func TestInstallerRemoveManagedInstallRemovesOnlyExpectedBinaryLinksAndStore(t *testing.T) {
	installer := NewInstaller()
	storeRoot := t.TempDir()
	storePath := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-abc123")
	require.NoError(t, os.MkdirAll(filepath.Join(storePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storePath, "artifact"), []byte("artifact"), 0o600))
	binDir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "foo")
	require.NoError(t, os.WriteFile(target, []byte("foo"), 0o755))
	linkPath := filepath.Join(binDir, "foo")
	require.NoError(t, os.Symlink(target, linkPath))

	err := installer.RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
		StoreRoot: storeRoot,
		BinRoot:   binDir,
		StorePath: storePath,
		Binaries: []app.InstalledBinary{
			{Name: "foo", LinkPath: linkPath, TargetPath: target},
		},
	})

	require.NoError(t, err)
	assert.NoFileExists(t, linkPath)
	assert.NoDirExists(t, storePath)
}

func TestInstallerRemoveManagedInstallRejectsLinkCleanupFailuresAndKeepsStore(t *testing.T) {
	installer := NewInstaller()
	storeRoot := t.TempDir()
	storePath := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-abc123")
	require.NoError(t, os.MkdirAll(filepath.Join(storePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storePath, "artifact"), []byte("artifact"), 0o600))
	binDir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "foo")
	require.NoError(t, os.WriteFile(target, []byte("foo"), 0o755))
	unsafePath := filepath.Join(binDir, "unsafe")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(unsafePath, []byte("not a symlink"), 0o644))

	err := installer.RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
		StoreRoot: storeRoot,
		BinRoot:   binDir,
		StorePath: storePath,
		Binaries: []app.InstalledBinary{
			{Name: "unsafe", LinkPath: unsafePath, TargetPath: target},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-symlink")
	assert.FileExists(t, unsafePath)
	assert.DirExists(t, storePath)

	wrongTarget := filepath.Join(targetDir, "wrong")
	require.NoError(t, os.WriteFile(wrongTarget, []byte("wrong"), 0o755))
	swappedPath := filepath.Join(binDir, "swapped")
	require.NoError(t, os.Symlink(wrongTarget, swappedPath))
	err = installer.RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
		StoreRoot: storeRoot,
		BinRoot:   binDir,
		StorePath: storePath,
		Binaries: []app.InstalledBinary{
			{Name: "swapped", LinkPath: swappedPath, TargetPath: target},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected target")
	assert.FileExists(t, swappedPath)
	assert.DirExists(t, storePath)
}

func TestInstallerRemoveManagedInstallRejectsPathsOutsideBinRoot(t *testing.T) {
	installer := NewInstaller()
	storeRoot := t.TempDir()
	storePath := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-abc123")
	require.NoError(t, os.MkdirAll(filepath.Join(storePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storePath, "artifact"), []byte("artifact"), 0o600))
	binDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "foo")
	require.NoError(t, os.WriteFile(target, []byte("foo"), 0o755))
	outsideLink := filepath.Join(t.TempDir(), "foo")
	require.NoError(t, os.Symlink(target, outsideLink))

	err := installer.RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
		StoreRoot: storeRoot,
		BinRoot:   binDir,
		StorePath: storePath,
		Binaries: []app.InstalledBinary{
			{Name: "foo", LinkPath: outsideLink, TargetPath: target},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not under bin root")
	assert.FileExists(t, outsideLink)
	assert.DirExists(t, storePath)

	err = installer.RemoveManagedInstall(context.Background(), app.RemoveManagedInstallRequest{
		StoreRoot: storeRoot,
		BinRoot:   binDir,
		StorePath: storePath,
		Binaries: []app.InstalledBinary{
			{Name: "relative", LinkPath: filepath.Join("bin", "foo"), TargetPath: target},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")
	assert.DirExists(t, storePath)
}

func TestInstallerRemoveManagedStoreRemovesStorePathUnderRoot(t *testing.T) {
	storeRoot := t.TempDir()
	storePath := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3", "sha256-abc123")
	versionDir := filepath.Dir(storePath)
	require.NoError(t, os.MkdirAll(filepath.Join(storePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storePath, "artifact"), []byte("artifact"), 0o600))
	untouched := filepath.Join(storeRoot, "github.com", "owner", "repo", "bar")
	require.NoError(t, os.MkdirAll(untouched, 0o755))

	err := NewInstaller().RemoveManagedStore(context.Background(), storeRoot, storePath)

	require.NoError(t, err)
	assert.NoDirExists(t, storePath)
	assert.NoDirExists(t, versionDir)
	assert.DirExists(t, untouched)
}

func TestInstallerRemoveManagedStoreKeepsVersionDirWhenAnotherDigestRemains(t *testing.T) {
	storeRoot := t.TempDir()
	versionDir := filepath.Join(storeRoot, "github.com", "owner", "repo", "foo", "1.2.3")
	storePath := filepath.Join(versionDir, "sha256-abc123")
	siblingStorePath := filepath.Join(versionDir, "sha256-def456")
	require.NoError(t, os.MkdirAll(filepath.Join(storePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storePath, "artifact"), []byte("artifact"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(siblingStorePath, "extracted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(siblingStorePath, "artifact"), []byte("artifact"), 0o600))

	err := NewInstaller().RemoveManagedStore(context.Background(), storeRoot, storePath)

	require.NoError(t, err)
	assert.NoDirExists(t, storePath)
	assert.DirExists(t, versionDir)
	assert.DirExists(t, siblingStorePath)
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
