package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/app"
)

// Installer owns install-time filesystem state.
type Installer struct{}

// NewInstaller creates a filesystem installer.
func NewInstaller() Installer {
	return Installer{}
}

// CreateDownloadDir creates a temporary directory for downloads and short-lived verification work.
func (Installer) CreateDownloadDir(ctx context.Context) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "ghd-download-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary download directory: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

// PublishVerifiedArtifact copies a verified artifact into an output directory without overwriting.
func (Installer) PublishVerifiedArtifact(ctx context.Context, sourcePath string, outputDir string, assetName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(sourcePath) == "" {
		return "", fmt.Errorf("source artifact path must be set")
	}
	name, err := cleanPathSegment("release asset name", assetName)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(outputDir) == "" {
		return "", fmt.Errorf("output directory must be set")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	destination := filepath.Join(outputDir, name)
	if err := copyFileExclusive(sourcePath, destination, 0o600); err != nil {
		return "", err
	}
	return destination, nil
}

// CreateStoreLayout creates the digest-keyed store layout and copies the artifact.
func (Installer) CreateStoreLayout(ctx context.Context, request app.StoreLayoutRequest) (app.StoreLayout, error) {
	if err := ctx.Err(); err != nil {
		return app.StoreLayout{}, err
	}
	layout, err := resolveStoreLayout(request)
	if err != nil {
		return app.StoreLayout{}, err
	}
	if err := os.MkdirAll(layout.storeRoot, 0o755); err != nil {
		return app.StoreLayout{}, fmt.Errorf("create store root: %w", err)
	}
	root, err := os.OpenRoot(layout.storeRoot)
	if err != nil {
		return app.StoreLayout{}, fmt.Errorf("open store root: %w", err)
	}
	defer root.Close()
	if err := rejectSymlinkComponents(root, layout.relStorePath); err != nil {
		return app.StoreLayout{}, err
	}
	if err := root.MkdirAll(layout.relStoreParent, 0o755); err != nil {
		return app.StoreLayout{}, fmt.Errorf("create store parent: %w", err)
	}
	if err := root.Mkdir(layout.relStorePath, 0o755); err != nil {
		if os.IsExist(err) {
			return app.StoreLayout{}, fmt.Errorf("store path %s already exists", layout.StorePath)
		}
		return app.StoreLayout{}, fmt.Errorf("create store path: %w", err)
	}
	if err := root.Mkdir(layout.relExtractedDir, 0o755); err != nil {
		_ = removeManagedStorePath(root, layout.relStorePath)
		if os.IsExist(err) {
			return app.StoreLayout{}, fmt.Errorf("extracted store directory %s already exists", layout.ExtractedDir)
		}
		return app.StoreLayout{}, fmt.Errorf("create extraction directory: %w", err)
	}
	if err := copyFileExclusiveRoot(root, request.ArtifactPath, layout.relArtifactPath, 0o600); err != nil {
		_ = removeManagedStorePath(root, layout.relStorePath)
		return app.StoreLayout{}, err
	}
	return app.StoreLayout{
		StorePath:    layout.StorePath,
		ArtifactPath: layout.ArtifactPath,
		ExtractedDir: layout.ExtractedDir,
	}, nil
}

// LinkBinaries links prepared binaries into the managed bin directory.
func (Installer) LinkBinaries(ctx context.Context, request app.LinkBinariesRequest) ([]app.InstalledBinary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	binRoot, planned, err := resolveManagedBinaryPlan(request)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(binRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create bin directory: %w", err)
	}
	if _, err := createManagedBinaryLinks(ctx, binRoot, planned, false); err != nil {
		return nil, err
	}
	return planned, nil
}

// RemoveManagedInstall removes managed binaries and store contents for one install.
func (Installer) RemoveManagedInstall(ctx context.Context, request app.RemoveManagedInstallRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, relStorePath, err := openManagedStoreRoot(request.StoreRoot, request.StorePath)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := rejectSymlinkComponents(root, relStorePath); err != nil {
		return err
	}
	if err := removeManagedBinaryLinks(ctx, request.BinRoot, request.Binaries); err != nil {
		return err
	}
	return removeManagedStorePath(root, relStorePath)
}

// ReplaceManagedBinaries swaps one active binary set for another with rollback on creation failure.
func (Installer) ReplaceManagedBinaries(ctx context.Context, request app.ReplaceManagedBinariesRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	binRoot, err := cleanBinRoot(request.BinDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binRoot, 0o755); err != nil {
		return fmt.Errorf("create bin directory: %w", err)
	}
	if err := validateManagedBinarySet(binRoot, request.Next); err != nil {
		return err
	}
	if err := ensureManagedBinaryLinks(ctx, binRoot, request.Previous); err != nil {
		return err
	}
	if err := removeManagedBinaryLinks(ctx, binRoot, request.Previous); err != nil {
		if restoreErr := restoreManagedBinaryLinks(context.WithoutCancel(ctx), binRoot, request.Previous); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous managed binaries: %w", restoreErr))
		}
		return err
	}
	if _, err := createManagedBinaryLinks(ctx, binRoot, request.Next, false); err != nil {
		if restoreErr := restoreManagedBinaryLinks(context.WithoutCancel(ctx), binRoot, request.Previous); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous managed binaries: %w", restoreErr))
		}
		return err
	}
	return nil
}

// RemoveManagedStore removes only the managed store directory for one install.
func (Installer) RemoveManagedStore(ctx context.Context, storeRoot string, storePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, relStorePath, err := openManagedStoreRoot(storeRoot, storePath)
	if err != nil {
		return err
	}
	defer root.Close()
	return removeManagedStorePath(root, relStorePath)
}

// VerifyManagedBinaryLink verifies that linkPath is a symlink to expectedTargetPath.
func (Installer) VerifyManagedBinaryLink(ctx context.Context, linkPath string, expectedTargetPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(linkPath) == "" {
		return fmt.Errorf("managed binary link path must be set")
	}
	if strings.TrimSpace(expectedTargetPath) == "" {
		return fmt.Errorf("managed binary target path must be set")
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		return fmt.Errorf("inspect managed binary link %s: %w", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("managed binary link %s is not a symlink", linkPath)
	}
	targetPath, err := os.Readlink(linkPath)
	if err != nil {
		return fmt.Errorf("read managed binary link %s: %w", linkPath, err)
	}
	if targetPath != expectedTargetPath {
		return fmt.Errorf("managed binary link %s points to %s, not %s", linkPath, targetPath, expectedTargetPath)
	}
	return nil
}

// CompareFiles verifies that both files have identical contents.
func (Installer) CompareFiles(ctx context.Context, path string, otherPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file %s: %w", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("file %s is not executable", path)
	}
	left, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}
	right, err := os.ReadFile(otherPath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", otherPath, err)
	}
	if !bytes.Equal(left, right) {
		return fmt.Errorf("file %s does not match %s", path, otherPath)
	}
	return nil
}

type managedBinaryLink struct {
	binary app.InstalledBinary
	rel    string
}

func resolveManagedBinaryPlan(request app.LinkBinariesRequest) (string, []app.InstalledBinary, error) {
	if strings.TrimSpace(request.BinDir) == "" {
		return "", nil, fmt.Errorf("bin directory must be set")
	}
	binRoot, err := cleanBinRoot(request.BinDir)
	if err != nil {
		return "", nil, err
	}
	if len(request.Binaries) == 0 {
		return "", nil, fmt.Errorf("at least one binary must be linked")
	}
	planned := make([]app.InstalledBinary, 0, len(request.Binaries))
	for _, binary := range request.Binaries {
		name, err := cleanPathSegment("binary name", binary.Name)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(binary.Path) == "" {
			return "", nil, fmt.Errorf("binary %q target path must be set", binary.Name)
		}
		planned = append(planned, app.InstalledBinary{
			Name:       name,
			LinkPath:   filepath.Join(binRoot, name),
			TargetPath: binary.Path,
		})
	}
	return binRoot, planned, nil
}

func validateManagedBinarySet(binRoot string, binaries []app.InstalledBinary) error {
	_, err := resolveManagedBinaryLinks(context.Background(), binRoot, binaries)
	return err
}

func resolveManagedBinaryLinks(ctx context.Context, binRoot string, binaries []app.InstalledBinary) ([]managedBinaryLink, error) {
	if len(binaries) == 0 {
		return nil, nil
	}
	links := make([]managedBinaryLink, 0, len(binaries))
	for _, binary := range binaries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := cleanPathSegment("binary name", binary.Name); err != nil {
			return nil, err
		}
		if strings.TrimSpace(binary.TargetPath) == "" {
			return nil, fmt.Errorf("binary %q target path must be set", binary.Name)
		}
		rel, err := cleanBinRelativePath(binRoot, binary.LinkPath)
		if err != nil {
			return nil, err
		}
		links = append(links, managedBinaryLink{binary: binary, rel: rel})
	}
	return links, nil
}

func ensureManagedBinaryLinks(ctx context.Context, binRoot string, binaries []app.InstalledBinary) error {
	if len(binaries) == 0 {
		return nil
	}
	binRoot, err := cleanBinRoot(binRoot)
	if err != nil {
		return err
	}
	links, err := resolveManagedBinaryLinks(ctx, binRoot, binaries)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(binRoot)
	if os.IsNotExist(err) {
		return fmt.Errorf("bin root %s does not exist", binRoot)
	}
	if err != nil {
		return fmt.Errorf("open bin root: %w", err)
	}
	defer root.Close()
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := root.Lstat(link.rel)
		if os.IsNotExist(err) {
			return fmt.Errorf("binary link %s does not exist", link.binary.LinkPath)
		}
		if err != nil {
			return fmt.Errorf("inspect binary link %s: %w", link.binary.LinkPath, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to remove non-symlink binary path %s", link.binary.LinkPath)
		}
		target, err := root.Readlink(link.rel)
		if err != nil {
			return fmt.Errorf("read binary link %s: %w", link.binary.LinkPath, err)
		}
		if target != link.binary.TargetPath {
			return fmt.Errorf("refusing to remove binary link %s with unexpected target %s", link.binary.LinkPath, target)
		}
	}
	return nil
}

func createManagedBinaryLinks(ctx context.Context, binRoot string, binaries []app.InstalledBinary, allowExistingExpected bool) ([]app.InstalledBinary, error) {
	if len(binaries) == 0 {
		return nil, nil
	}
	links, err := resolveManagedBinaryLinks(ctx, binRoot, binaries)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(binRoot)
	if err != nil {
		return nil, fmt.Errorf("open bin root: %w", err)
	}
	defer root.Close()
	created := make([]app.InstalledBinary, 0, len(links))
	cleanup := func() {
		_ = removeManagedBinaryLinks(context.WithoutCancel(ctx), binRoot, created)
	}
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			cleanup()
			return nil, err
		}
		if err := root.Symlink(link.binary.TargetPath, link.rel); err != nil {
			if os.IsExist(err) && allowExistingExpected {
				info, statErr := root.Lstat(link.rel)
				if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
					target, readErr := root.Readlink(link.rel)
					if readErr == nil && target == link.binary.TargetPath {
						continue
					}
				}
			}
			cleanup()
			if os.IsExist(err) {
				return nil, fmt.Errorf("binary link %s already exists", link.binary.LinkPath)
			}
			return nil, fmt.Errorf("link binary %s: %w", link.binary.Name, err)
		}
		created = append(created, link.binary)
	}
	return created, nil
}

func restoreManagedBinaryLinks(ctx context.Context, binRoot string, binaries []app.InstalledBinary) error {
	_, err := createManagedBinaryLinks(ctx, binRoot, binaries, true)
	return err
}

func removeManagedBinaryLinks(ctx context.Context, binRoot string, binaries []app.InstalledBinary) error {
	if len(binaries) == 0 {
		return nil
	}
	binRoot, err := cleanBinRoot(binRoot)
	if err != nil {
		return err
	}
	links, err := resolveManagedBinaryLinks(ctx, binRoot, binaries)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(binRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open bin root: %w", err)
	}
	defer root.Close()
	var errs []error
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		binary := link.binary
		info, err := root.Lstat(link.rel)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("inspect binary link %s: %w", binary.LinkPath, err))
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			errs = append(errs, fmt.Errorf("refusing to remove non-symlink binary path %s", binary.LinkPath))
			continue
		}
		target, err := root.Readlink(link.rel)
		if err != nil {
			errs = append(errs, fmt.Errorf("read binary link %s: %w", binary.LinkPath, err))
			continue
		}
		if target != binary.TargetPath {
			errs = append(errs, fmt.Errorf("refusing to remove binary link %s with unexpected target %s", binary.LinkPath, target))
			continue
		}
		if err := root.Remove(link.rel); err != nil {
			errs = append(errs, fmt.Errorf("remove binary link %s: %w", binary.LinkPath, err))
		}
	}
	return errors.Join(errs...)
}

// WriteInstallMetadata writes install.json into storePath.
func (Installer) WriteInstallMetadata(ctx context.Context, storePath string, record app.InstallRecord) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(storePath) == "" {
		return "", fmt.Errorf("store path must be set")
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode install metadata: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(storePath, "install.json", data, 0o644)
}

func copyFileExclusive(source string, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("output artifact %s already exists", destination)
		}
		return fmt.Errorf("create output artifact: %w", err)
	}
	removeOutput := true
	defer func() {
		if removeOutput {
			_ = os.Remove(destination)
		}
	}()

	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return fmt.Errorf("copy output artifact: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close output artifact: %w", err)
	}
	removeOutput = false
	return nil
}

func copyFileExclusiveRoot(root *os.Root, source string, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer input.Close()

	output, err := root.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("store artifact %s already exists", destination)
		}
		return fmt.Errorf("create store artifact: %w", err)
	}
	removeOutput := true
	defer func() {
		if removeOutput {
			_ = root.Remove(destination)
		}
	}()

	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return fmt.Errorf("copy store artifact: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close store artifact: %w", err)
	}
	removeOutput = false
	return nil
}

func openManagedStoreRoot(storeRoot string, storePath string) (*os.Root, string, error) {
	storeRoot, err := cleanStoreRoot(storeRoot)
	if err != nil {
		return nil, "", err
	}
	rel, err := cleanStoreRelativePath(storeRoot, storePath)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(storeRoot)
	if err != nil {
		return nil, "", fmt.Errorf("open store root: %w", err)
	}
	return root, rel, nil
}

func removeManagedStorePath(root *os.Root, relStorePath string) error {
	if err := rejectSymlinkComponents(root, relStorePath); err != nil {
		return err
	}
	if err := root.RemoveAll(relStorePath); err != nil {
		return fmt.Errorf("remove managed store path: %w", err)
	}
	parentRel := filepath.Dir(relStorePath)
	if parentRel == "." || parentRel == "" {
		return nil
	}
	parentEmpty, err := managedStoreDirectoryEmpty(root, parentRel)
	if err != nil {
		return err
	}
	if !parentEmpty {
		return nil
	}
	if err := root.Remove(parentRel); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove empty managed store parent: %w", err)
	}
	return nil
}

func managedStoreDirectoryEmpty(root *os.Root, relDir string) (bool, error) {
	dir, err := root.Open(relDir)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("open managed store parent: %w", err)
	}
	defer dir.Close()
	_, err = dir.ReadDir(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read managed store parent: %w", err)
	}
	return false, nil
}

type resolvedStoreLayout struct {
	storeRoot       string
	relStorePath    string
	relStoreParent  string
	relArtifactPath string
	relExtractedDir string
	StorePath       string
	ArtifactPath    string
	ExtractedDir    string
}

func resolveStoreLayout(request app.StoreLayoutRequest) (resolvedStoreLayout, error) {
	storeRoot, err := cleanStoreRoot(request.StoreRoot)
	if err != nil {
		return resolvedStoreLayout{}, err
	}
	if err := validateStoreDigest(request.AssetDigest.Algorithm, request.AssetDigest.Hex); err != nil {
		return resolvedStoreLayout{}, err
	}
	owner, err := cleanPathSegment("repository owner", request.Repository.Owner)
	if err != nil {
		return resolvedStoreLayout{}, err
	}
	repo, err := cleanPathSegment("repository name", request.Repository.Name)
	if err != nil {
		return resolvedStoreLayout{}, err
	}
	pkg, err := cleanPathSegment("package name", request.PackageName.String())
	if err != nil {
		return resolvedStoreLayout{}, err
	}
	version, err := cleanPathSegment("version", request.Version.String())
	if err != nil {
		return resolvedStoreLayout{}, err
	}
	if strings.TrimSpace(request.ArtifactPath) == "" {
		return resolvedStoreLayout{}, fmt.Errorf("artifact path must be set")
	}
	relStorePath := filepath.Join(
		"github.com",
		owner,
		repo,
		pkg,
		version,
		request.AssetDigest.Algorithm+"-"+request.AssetDigest.Hex,
	)
	storePath := filepath.Join(storeRoot, relStorePath)
	relArtifactPath := filepath.Join(relStorePath, "artifact")
	relExtractedDir := filepath.Join(relStorePath, "extracted")
	return resolvedStoreLayout{
		storeRoot:       storeRoot,
		relStorePath:    relStorePath,
		relStoreParent:  filepath.Dir(relStorePath),
		relArtifactPath: relArtifactPath,
		relExtractedDir: relExtractedDir,
		StorePath:       storePath,
		ArtifactPath:    filepath.Join(storeRoot, relArtifactPath),
		ExtractedDir:    filepath.Join(storeRoot, relExtractedDir),
	}, nil
}

func cleanStoreRoot(value string) (string, error) {
	return cleanManagedRoot("store root", value)
}

func cleanBinRoot(value string) (string, error) {
	return cleanManagedRoot("bin root", value)
}

func cleanManagedRoot(label string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must be set", label)
	}
	root, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	if root == string(os.PathSeparator) {
		return "", fmt.Errorf("refusing to use unsafe %s %s", label, value)
	}
	return root, nil
}

func cleanStoreRelativePath(storeRoot string, storePath string) (string, error) {
	storePath = strings.TrimSpace(storePath)
	if storePath == "" {
		return "", fmt.Errorf("store path must be set")
	}
	if !filepath.IsAbs(storePath) {
		return "", fmt.Errorf("recorded store path %s must be absolute", storePath)
	}
	absStorePath := filepath.Clean(storePath)
	rel, err := filepath.Rel(storeRoot, absStorePath)
	if err != nil {
		return "", fmt.Errorf("compare store path to root: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("store path %s is not under store root %s", storePath, storeRoot)
	}
	return filepath.Clean(rel), nil
}

func cleanBinRelativePath(binRoot string, linkPath string) (string, error) {
	linkPath = strings.TrimSpace(linkPath)
	if linkPath == "" {
		return "", fmt.Errorf("binary link path must be set")
	}
	if !filepath.IsAbs(linkPath) {
		return "", fmt.Errorf("recorded binary link path %s must be absolute", linkPath)
	}
	absLinkPath := filepath.Clean(linkPath)
	rel, err := filepath.Rel(binRoot, absLinkPath)
	if err != nil {
		return "", fmt.Errorf("compare binary link path to root: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("binary link path %s is not under bin root %s", linkPath, binRoot)
	}
	return filepath.Clean(rel), nil
}

func rejectSymlinkComponents(root *os.Root, rel string) error {
	current := ""
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect store path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to remove store path through symlink component %s", current)
		}
	}
	return nil
}

func cleanPathSegment(label string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must be set", label)
	}
	if value == "." || value == ".." {
		return "", fmt.Errorf("%s %q must be a path segment", label, value)
	}
	if strings.ContainsAny(value, `/\`) {
		return "", fmt.Errorf("%s %q must not contain path separators", label, value)
	}
	if !filepath.IsLocal(value) {
		return "", fmt.Errorf("%s %q must be local", label, value)
	}
	return value, nil
}

func validateStoreDigest(algorithm string, value string) error {
	if _, err := cleanPathSegment("digest algorithm", algorithm); err != nil {
		return err
	}
	if _, err := cleanPathSegment("digest value", value); err != nil {
		return err
	}
	return nil
}

func writeFileAtomic(dir string, name string, data []byte, mode os.FileMode) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	finalPath := filepath.Join(dir, name)
	temp, err := os.CreateTemp(dir, "."+name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary %s: %w", name, err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write temporary %s: %w", name, err)
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("chmod temporary %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temporary %s: %w", name, err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", fmt.Errorf("commit %s: %w", name, err)
	}
	removeTemp = false
	return finalPath, nil
}
