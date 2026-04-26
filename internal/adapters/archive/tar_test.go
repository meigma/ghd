package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/manifest"
)

func TestTarGzipExtractorExtractsConfiguredBinary(t *testing.T) {
	archivePath := writeTarGzip(t, []tarTestEntry{
		{name: "bin", typeflag: tar.TypeDir, mode: 0o755},
		{name: "bin/foo", body: "hello\n", typeflag: tar.TypeReg, mode: 0o755},
	})
	destination := t.TempDir()

	result, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   archivePath,
		DestinationDir: destination,
		Binaries:       []manifest.Binary{{Path: "bin/foo"}},
	})

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "foo", result[0].Name)
	assert.Equal(t, filepath.Join("bin", "foo"), result[0].RelativePath)
	assert.True(t, filepath.IsAbs(result[0].Path))
	data, err := os.ReadFile(filepath.Join(destination, "bin", "foo"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(data))
}

func TestTarGzipExtractorDoesNotMaterializeUnconfiguredRegularFiles(t *testing.T) {
	archivePath := writeTarGzip(t, []tarTestEntry{
		{name: "bin", typeflag: tar.TypeDir, mode: 0o755},
		{name: "bin/foo", body: "hello\n", typeflag: tar.TypeReg, mode: 0o755},
		{name: "share", typeflag: tar.TypeDir, mode: 0o755},
		{name: "share/readme.txt", body: "docs\n", typeflag: tar.TypeReg, mode: 0o644},
	})
	destination := t.TempDir()

	_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   archivePath,
		DestinationDir: destination,
		Binaries:       []manifest.Binary{{Path: "bin/foo"}},
	})

	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(destination, "bin", "foo"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(data))
	_, err = os.Stat(filepath.Join(destination, "share", "readme.txt"))
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestTarGzipExtractorRejectsSkippedPathThatBlocksConfiguredBinary(t *testing.T) {
	archivePath := writeTarGzip(t, []tarTestEntry{
		{name: "bin", body: "not-a-directory", typeflag: tar.TypeReg, mode: 0o644},
		{name: "bin/foo", body: "hello\n", typeflag: tar.TypeReg, mode: 0o755},
	})

	_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   archivePath,
		DestinationDir: t.TempDir(),
		Binaries:       []manifest.Binary{{Path: "bin/foo"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `archive entry "bin" conflicts with configured binary path "bin/foo"`)
}

func TestTarGzipExtractorMasksWritablePermissionBits(t *testing.T) {
	archivePath := writeTarGzip(t, []tarTestEntry{
		{name: "bin", typeflag: tar.TypeDir, mode: 0o777},
		{name: "bin/foo", body: "hello\n", typeflag: tar.TypeReg, mode: 0o777},
	})
	destination := t.TempDir()

	_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   archivePath,
		DestinationDir: destination,
		Binaries:       []manifest.Binary{{Path: "bin/foo"}},
	})

	require.NoError(t, err)
	dirInfo, err := os.Stat(filepath.Join(destination, "bin"))
	require.NoError(t, err)
	fileInfo, err := os.Stat(filepath.Join(destination, "bin", "foo"))
	require.NoError(t, err)
	assert.Zero(t, dirInfo.Mode().Perm()&0o022, "directory should not be group/world writable")
	assert.Zero(t, fileInfo.Mode().Perm()&0o022, "file should not be group/world writable")
	assert.NotZero(t, fileInfo.Mode().Perm()&0o111, "executable bit should be preserved")
}

func TestTarGzipExtractorVerifiesGzipStream(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T) string
		wantError string
	}{
		{
			name: "corrupt trailer",
			prepare: func(t *testing.T) string {
				path := writeTarGzip(t, []tarTestEntry{
					{name: "bin/foo", body: "hello\n", typeflag: tar.TypeReg, mode: 0o755},
				})
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NotEmpty(t, data)
				data[len(data)-1] ^= 0xff
				require.NoError(t, os.WriteFile(path, data, 0o600))
				return path
			},
			wantError: "verify gzip stream",
		},
		{
			name: "trailing gzip member",
			prepare: func(t *testing.T) string {
				path := writeTarGzip(t, []tarTestEntry{
					{name: "bin/foo", body: "hello\n", typeflag: tar.TypeReg, mode: 0o755},
				})
				appendGzipMember(t, path, "trailing")
				return path
			},
			wantError: "trailing data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
				ArtifactPath:   tt.prepare(t),
				DestinationDir: t.TempDir(),
				Binaries:       []manifest.Binary{{Path: "bin/foo"}},
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

func TestTarGzipExtractorRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name      string
		entry     tarTestEntry
		wantError string
	}{
		{
			name:      "parent traversal",
			entry:     tarTestEntry{name: "../evil", body: "x", typeflag: tar.TypeReg, mode: 0o755},
			wantError: "must not contain parent directory segments",
		},
		{
			name:      "absolute path",
			entry:     tarTestEntry{name: "/evil", body: "x", typeflag: tar.TypeReg, mode: 0o755},
			wantError: "must be local",
		},
		{
			name:      "backslash path",
			entry:     tarTestEntry{name: `bin\evil`, body: "x", typeflag: tar.TypeReg, mode: 0o755},
			wantError: "backslashes",
		},
		{
			name:      "symlink",
			entry:     tarTestEntry{name: "bin/foo", linkname: "/tmp/foo", typeflag: tar.TypeSymlink, mode: 0o777},
			wantError: "unsupported type",
		},
		{
			name:      "hardlink",
			entry:     tarTestEntry{name: "bin/foo", linkname: "bin/bar", typeflag: tar.TypeLink, mode: 0o777},
			wantError: "unsupported type",
		},
		{
			name:      "fifo",
			entry:     tarTestEntry{name: "bin/foo", typeflag: tar.TypeFifo, mode: 0o777},
			wantError: "unsupported type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivePath := writeTarGzip(t, []tarTestEntry{tt.entry})

			_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
				ArtifactPath:   archivePath,
				DestinationDir: t.TempDir(),
				Binaries:       []manifest.Binary{{Path: "bin/foo"}},
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

func TestTarGzipExtractorRejectsUnsupportedArchiveType(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "archive.zip")
	require.NoError(t, os.WriteFile(archivePath, []byte("zip"), 0o600))

	_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
		ArtifactPath:   archivePath,
		DestinationDir: t.TempDir(),
		Binaries:       []manifest.Binary{{Path: "bin/foo"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported archive type")
}

func TestTarGzipExtractorRejectsMissingOrNonExecutableBinary(t *testing.T) {
	tests := []struct {
		name      string
		entry     tarTestEntry
		wantError string
	}{
		{
			name:      "missing",
			entry:     tarTestEntry{name: "bin/bar", body: "x", typeflag: tar.TypeReg, mode: 0o755},
			wantError: "not found",
		},
		{
			name:      "not executable",
			entry:     tarTestEntry{name: "bin/foo", body: "x", typeflag: tar.TypeReg, mode: 0o644},
			wantError: "not executable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivePath := writeTarGzip(t, []tarTestEntry{tt.entry})

			_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
				ArtifactPath:   archivePath,
				DestinationDir: t.TempDir(),
				Binaries:       []manifest.Binary{{Path: "bin/foo"}},
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

func TestTarGzipExtractorRejectsTarBombLimits(t *testing.T) {
	t.Run("uncompressed size", func(t *testing.T) {
		archivePath := writeOversizedTarGzip(t)

		_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
			ArtifactPath:   archivePath,
			DestinationDir: t.TempDir(),
			Binaries:       []manifest.Binary{{Path: "bin/foo"}},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "expands beyond")
	})

	t.Run("overflow shape", func(t *testing.T) {
		archivePath := writeOverflowTarGzip(t)

		_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
			ArtifactPath:   archivePath,
			DestinationDir: t.TempDir(),
			Binaries:       []manifest.Binary{{Path: "bin/foo"}},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "expands beyond")
	})

	t.Run("entry count", func(t *testing.T) {
		entries := make([]tarTestEntry, MaxEntries+1)
		for i := range entries {
			entries[i] = tarTestEntry{
				name:     filepath.ToSlash(filepath.Join("dirs", fmt.Sprintf("d%d", i+1))),
				typeflag: tar.TypeDir,
				mode:     0o755,
			}
		}
		archivePath := writeTarGzip(t, entries)

		_, err := NewTarGzipExtractor().MaterializeBinaries(context.Background(), app.ArtifactMaterializationRequest{
			ArtifactPath:   archivePath,
			DestinationDir: t.TempDir(),
			Binaries:       []manifest.Binary{{Path: "bin/foo"}},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "more than")
	})
}

type tarTestEntry struct {
	name     string
	body     string
	linkname string
	typeflag byte
	mode     int64
}

func writeTarGzip(t *testing.T, entries []tarTestEntry) string {
	t.Helper()

	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	file, err := os.Create(archivePath)
	require.NoError(t, err)
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		size := int64(len(entry.body))
		if entry.typeflag != tar.TypeReg {
			size = 0
		}
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{
			Name:     entry.name,
			Linkname: entry.linkname,
			Typeflag: entry.typeflag,
			Mode:     entry.mode,
			Size:     size,
		}))
		if size > 0 {
			_, err := tarWriter.Write([]byte(entry.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, file.Close())
	return archivePath
}

func writeOversizedTarGzip(t *testing.T) string {
	t.Helper()

	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	file, err := os.Create(archivePath)
	require.NoError(t, err)
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{
		Name:     "bin/foo",
		Typeflag: tar.TypeReg,
		Mode:     0o755,
		Size:     MaxUncompressedBytes + 1,
	}))
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, file.Close())
	return archivePath
}

func writeOverflowTarGzip(t *testing.T) string {
	t.Helper()

	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	file, err := os.Create(archivePath)
	require.NoError(t, err)
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{
		Name:     "bin/small",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     1,
	}))
	_, err = tarWriter.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{
		Name:     "bin/foo",
		Typeflag: tar.TypeReg,
		Mode:     0o755,
		Size:     math.MaxInt64,
	}))
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, file.Close())
	return archivePath
}

func appendGzipMember(t *testing.T, path string, body string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	gzipWriter := gzip.NewWriter(file)
	_, err = gzipWriter.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, file.Close())
}
