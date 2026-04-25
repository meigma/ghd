package archive

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/manifest"
)

func TestMaterializerMaterializeBinariesDispatchesTarGzip(t *testing.T) {
	archivePath := writeTarGzip(t, []tarTestEntry{
		{name: "bin", typeflag: tar.TypeDir, mode: 0o755},
		{name: "bin/foo", body: "hello\n", typeflag: tar.TypeReg, mode: 0o755},
	})
	destination := t.TempDir()

	result, err := NewMaterializer().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   archivePath,
		AssetName:      "foo_1.2.3_darwin_arm64.tar.gz",
		DestinationDir: destination,
		Binaries:       []manifest.Binary{{Path: "bin/foo"}},
	})

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, filepath.Join("bin", "foo"), result[0].RelativePath)
	data, err := os.ReadFile(filepath.Join(destination, "bin", "foo"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(data))
}

func TestMaterializerMaterializeBinariesDirectBinaryAsset(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "foo_1.2.3_darwin_arm64")
	require.NoError(t, os.WriteFile(artifactPath, []byte("binary"), 0o600))
	destination := t.TempDir()

	result, err := NewMaterializer().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   artifactPath,
		AssetName:      "foo_1.2.3_darwin_arm64",
		DestinationDir: destination,
		Binaries:       []manifest.Binary{{Path: "bin/foo"}},
	})

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "foo", result[0].Name)
	assert.Equal(t, filepath.Join("bin", "foo"), result[0].RelativePath)
	assert.Equal(t, filepath.Join(destination, "bin", "foo"), result[0].Path)
	data, err := os.ReadFile(result[0].Path)
	require.NoError(t, err)
	assert.Equal(t, "binary", string(data))
	sourceData, err := os.ReadFile(artifactPath)
	require.NoError(t, err)
	assert.Equal(t, "binary", string(sourceData))
	info, err := os.Stat(result[0].Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestMaterializerMaterializeBinariesRejectsDirectAssetWithMultipleBinaries(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "foo_1.2.3_darwin_arm64")
	require.NoError(t, os.WriteFile(artifactPath, []byte("binary"), 0o600))

	_, err := NewMaterializer().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   artifactPath,
		AssetName:      "foo_1.2.3_darwin_arm64",
		DestinationDir: t.TempDir(),
		Binaries: []manifest.Binary{
			{Path: "bin/foo"},
			{Path: "bin/bar"},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `cannot satisfy 2 configured binaries`)
}
