package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/app"
)

// Materializer prepares configured binaries from verified artifacts.
type Materializer struct {
	tar TarGzipExtractor
}

// NewMaterializer creates an artifact materializer that supports direct binaries and .tar.gz archives.
func NewMaterializer() Materializer {
	return Materializer{tar: NewTarGzipExtractor()}
}

// MaterializeBinaries prepares configured binaries from one verified artifact.
func (m Materializer) MaterializeBinaries(
	ctx context.Context,
	request app.ArtifactMaterializationRequest,
) ([]app.MaterializedBinary, error) {
	assetName := strings.TrimSpace(request.AssetName)
	if assetName == "" {
		assetName = request.ArtifactPath
	}
	if strings.HasSuffix(assetName, ".tar.gz") {
		return m.tar.MaterializeBinaries(ctx, request)
	}
	return materializeDirectBinary(ctx, request, assetName)
}

func materializeDirectBinary(
	ctx context.Context,
	request app.ArtifactMaterializationRequest,
	assetName string,
) ([]app.MaterializedBinary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.ArtifactPath) == "" {
		return nil, errors.New("artifact path must be set")
	}
	if strings.TrimSpace(request.DestinationDir) == "" {
		return nil, errors.New("extraction destination must be set")
	}
	if len(request.Binaries) != 1 {
		return nil, fmt.Errorf(
			"non-archive asset %q cannot satisfy %d configured binaries",
			assetName,
			len(request.Binaries),
		)
	}
	binary := request.Binaries[0]
	if err := binary.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(request.DestinationDir, defaultDirMode); err != nil {
		return nil, fmt.Errorf("create extraction destination: %w", err)
	}
	root, err := os.OpenRoot(request.DestinationDir)
	if err != nil {
		return nil, fmt.Errorf("open extraction root: %w", err)
	}
	defer root.Close()

	relativePath := cleanManifestPath(binary.Path)
	parent := filepath.Dir(relativePath)
	if parent != "." {
		if mkdirErr := root.MkdirAll(parent, defaultDirMode); mkdirErr != nil {
			return nil, fmt.Errorf("create binary parent for %q: %w", binary.Path, mkdirErr)
		}
	}

	source, err := os.Open(request.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact %s is not a regular file", request.ArtifactPath)
	}

	target, err := root.OpenFile(relativePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, executableFileMode)
	if err != nil {
		return nil, fmt.Errorf("create binary %q: %w", binary.Path, err)
	}
	removeTarget := true
	defer func() {
		if removeTarget {
			_ = root.Remove(relativePath)
		}
	}()
	if _, copyErr := io.Copy(target, source); copyErr != nil {
		_ = target.Close()
		return nil, fmt.Errorf("write binary %q: %w", binary.Path, copyErr)
	}
	if closeErr := target.Close(); closeErr != nil {
		return nil, fmt.Errorf("close binary %q: %w", binary.Path, closeErr)
	}
	removeTarget = false

	absolutePath, err := filepath.Abs(filepath.Join(request.DestinationDir, relativePath))
	if err != nil {
		return nil, fmt.Errorf("resolve configured binary %q: %w", binary.Path, err)
	}
	return []app.MaterializedBinary{{
		Name:         filepath.Base(relativePath),
		RelativePath: relativePath,
		Path:         absolutePath,
	}}, nil
}
