package archive

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/manifest"
)

const (
	// MaxUncompressedBytes is the maximum total regular-file bytes extracted.
	MaxUncompressedBytes int64 = 100 * 1024 * 1024
	// MaxEntries is the maximum number of entries accepted from one archive.
	MaxEntries = 10_000
)

// TarGzipExtractor extracts .tar.gz archives.
type TarGzipExtractor struct{}

// NewTarGzipExtractor creates a tar.gz archive extractor.
func NewTarGzipExtractor() TarGzipExtractor {
	return TarGzipExtractor{}
}

// MaterializeBinaries extracts a verified tar.gz archive.
func (TarGzipExtractor) MaterializeBinaries(ctx context.Context, request app.ArtifactMaterializationRequest) ([]app.MaterializedBinary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	archiveName := request.AssetName
	if archiveName == "" {
		archiveName = request.ArtifactPath
	}
	if !strings.HasSuffix(archiveName, ".tar.gz") {
		return nil, fmt.Errorf("unsupported archive type for %s", archiveName)
	}
	if strings.TrimSpace(request.DestinationDir) == "" {
		return nil, fmt.Errorf("extraction destination must be set")
	}
	if len(request.Binaries) == 0 {
		return nil, fmt.Errorf("at least one binary must be configured")
	}
	plan, err := newExtractionPlan(request.Binaries)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(request.DestinationDir, 0o755); err != nil {
		return nil, fmt.Errorf("create extraction destination: %w", err)
	}

	file, err := os.Open(request.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	buffered := bufio.NewReader(file)
	gzipReader, err := gzip.NewReader(buffered)
	if err != nil {
		return nil, fmt.Errorf("open gzip stream: %w", err)
	}
	gzipReader.Multistream(false)

	root, err := os.OpenRoot(request.DestinationDir)
	if err != nil {
		_ = gzipReader.Close()
		return nil, fmt.Errorf("open extraction root: %w", err)
	}
	defer root.Close()

	if err := extractTar(ctx, root, tar.NewReader(gzipReader), plan); err != nil {
		_ = gzipReader.Close()
		return nil, err
	}
	if err := verifyGzipStream(gzipReader, buffered); err != nil {
		return nil, err
	}
	binaries, err := validateBinaries(root, request.DestinationDir, request.Binaries)
	if err != nil {
		return nil, err
	}
	return binaries, nil
}

type extractionPlan struct {
	targets []string
	target  map[string]struct{}
	dir     map[string]struct{}
}

func newExtractionPlan(binaries []manifest.Binary) (extractionPlan, error) {
	plan := extractionPlan{
		target: make(map[string]struct{}, len(binaries)),
		dir:    make(map[string]struct{}, len(binaries)),
	}
	for _, binary := range binaries {
		if err := binary.Validate(); err != nil {
			return extractionPlan{}, err
		}
		target := cleanManifestPath(binary.Path)
		if _, ok := plan.target[target]; !ok {
			plan.target[target] = struct{}{}
			plan.targets = append(plan.targets, target)
		}
		for parent := filepath.Dir(target); parent != "."; parent = filepath.Dir(parent) {
			plan.dir[parent] = struct{}{}
		}
	}
	return plan, nil
}

func (p extractionPlan) wantsFile(name string) bool {
	_, ok := p.target[name]
	return ok
}

func (p extractionPlan) wantsDir(name string) bool {
	if _, ok := p.dir[name]; ok {
		return true
	}
	_, ok := p.target[name]
	return ok
}

func (p extractionPlan) conflictingTarget(name string) (string, bool) {
	for _, target := range p.targets {
		if target == name {
			continue
		}
		if isArchiveAncestor(name, target) || isArchiveAncestor(target, name) {
			return target, true
		}
	}
	return "", false
}

func isArchiveAncestor(parent string, child string) bool {
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

func extractTar(ctx context.Context, root *os.Root, reader *tar.Reader, plan extractionPlan) error {
	var totalBytes int64
	var entries int
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if errors.Is(err, tar.ErrInsecurePath) {
			if header != nil {
				if _, cleanErr := cleanArchiveName(header.Name); cleanErr != nil {
					return cleanErr
				}
			}
			return fmt.Errorf("archive contains insecure path: %w", err)
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		entries++
		if entries > MaxEntries {
			return fmt.Errorf("archive contains more than %d entries", MaxEntries)
		}
		name, err := cleanArchiveName(header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if target, ok := plan.conflictingTarget(name); ok && !plan.wantsDir(name) {
				return fmt.Errorf("archive entry %q conflicts with configured binary path %q", header.Name, filepath.ToSlash(target))
			}
			if !plan.wantsDir(name) {
				continue
			}
			if err := root.MkdirAll(name, safeDirMode(header)); err != nil {
				return fmt.Errorf("create archive directory %q: %w", header.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 {
				return fmt.Errorf("archive file %q has negative size", header.Name)
			}
			if header.Size > MaxUncompressedBytes-totalBytes {
				return fmt.Errorf("archive expands beyond %d bytes", MaxUncompressedBytes)
			}
			totalBytes += header.Size
			if target, ok := plan.conflictingTarget(name); ok && !plan.wantsFile(name) {
				return fmt.Errorf("archive entry %q conflicts with configured binary path %q", header.Name, filepath.ToSlash(target))
			}
			if !plan.wantsFile(name) {
				continue
			}
			if err := writeRegularFile(root, reader, header, name); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q has unsupported type %q", header.Name, header.Typeflag)
		}
	}
}

func verifyGzipStream(reader *gzip.Reader, source *bufio.Reader) error {
	if _, err := io.Copy(io.Discard, reader); err != nil {
		_ = reader.Close()
		return fmt.Errorf("verify gzip stream: %w", err)
	}
	if err := reader.Close(); err != nil {
		return fmt.Errorf("close gzip stream: %w", err)
	}
	if _, err := source.Peek(1); err == nil {
		return fmt.Errorf("gzip stream has trailing data")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("inspect trailing gzip data: %w", err)
	}
	return nil
}

func writeRegularFile(root *os.Root, reader *tar.Reader, header *tar.Header, name string) error {
	parent := filepath.Dir(name)
	if parent != "." {
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create archive parent for %q: %w", header.Name, err)
		}
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, safeFileMode(header))
	if err != nil {
		return fmt.Errorf("create archive file %q: %w", header.Name, err)
	}
	removeFile := true
	defer func() {
		if removeFile {
			_ = root.Remove(name)
		}
	}()

	if _, err := io.CopyN(file, reader, header.Size); err != nil {
		_ = file.Close()
		return fmt.Errorf("write archive file %q: %w", header.Name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close archive file %q: %w", header.Name, err)
	}
	removeFile = false
	return nil
}

func validateBinaries(root *os.Root, destination string, binaries []manifest.Binary) ([]app.MaterializedBinary, error) {
	extracted := make([]app.MaterializedBinary, 0, len(binaries))
	for _, binary := range binaries {
		if err := binary.Validate(); err != nil {
			return nil, err
		}
		relativePath := cleanManifestPath(binary.Path)
		info, err := root.Stat(relativePath)
		if err != nil {
			return nil, fmt.Errorf("configured binary %q not found: %w", binary.Path, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("configured binary %q is a directory", binary.Path)
		}
		if info.Mode().Perm()&0o111 == 0 {
			return nil, fmt.Errorf("configured binary %q is not executable", binary.Path)
		}
		absolutePath, err := filepath.Abs(filepath.Join(destination, relativePath))
		if err != nil {
			return nil, fmt.Errorf("resolve configured binary %q: %w", binary.Path, err)
		}
		extracted = append(extracted, app.MaterializedBinary{
			Name:         filepath.Base(relativePath),
			RelativePath: relativePath,
			Path:         absolutePath,
		})
	}
	return extracted, nil
}

func cleanArchiveName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("archive contains an empty path")
	}
	if strings.Contains(name, "\\") {
		return "", fmt.Errorf("archive path %q contains backslashes", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("archive path %q must not contain ..", name)
		}
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." {
		return "", fmt.Errorf("archive path %q must not resolve to the extraction root", name)
	}
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("archive path %q must be local", name)
	}
	return clean, nil
}

func cleanManifestPath(value string) string {
	return filepath.Clean(filepath.FromSlash(path.Clean(strings.ReplaceAll(value, "\\", "/"))))
}

func safeFileMode(header *tar.Header) os.FileMode {
	mode := os.FileMode(header.Mode).Perm()
	if mode == 0 {
		return 0o644
	}
	return mode &^ 0o022
}

func safeDirMode(header *tar.Header) os.FileMode {
	mode := os.FileMode(header.Mode).Perm()
	if mode == 0 {
		return 0o755
	}
	return mode &^ 0o022
}
