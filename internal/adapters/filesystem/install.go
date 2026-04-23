package filesystem

import (
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

// CreateDownloadDir creates a temporary directory for install downloads.
func (Installer) CreateDownloadDir(ctx context.Context) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "ghd-install-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary download directory: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

// CreateStoreLayout creates the digest-keyed store layout and copies the artifact.
func (Installer) CreateStoreLayout(ctx context.Context, request app.StoreLayoutRequest) (app.StoreLayout, error) {
	if err := ctx.Err(); err != nil {
		return app.StoreLayout{}, err
	}
	storeRoot, err := cleanStoreRoot(request.StoreRoot)
	if err != nil {
		return app.StoreLayout{}, err
	}
	if err := validateStoreDigest(request.AssetDigest.Algorithm, request.AssetDigest.Hex); err != nil {
		return app.StoreLayout{}, err
	}
	owner, err := cleanPathSegment("repository owner", request.Repository.Owner)
	if err != nil {
		return app.StoreLayout{}, err
	}
	repo, err := cleanPathSegment("repository name", request.Repository.Name)
	if err != nil {
		return app.StoreLayout{}, err
	}
	pkg, err := cleanPathSegment("package name", request.PackageName)
	if err != nil {
		return app.StoreLayout{}, err
	}
	version, err := cleanPathSegment("version", request.Version)
	if err != nil {
		return app.StoreLayout{}, err
	}
	if strings.TrimSpace(request.ArtifactPath) == "" {
		return app.StoreLayout{}, fmt.Errorf("artifact path must be set")
	}

	storePath := filepath.Join(
		storeRoot,
		"github.com",
		owner,
		repo,
		pkg,
		version,
		request.AssetDigest.Algorithm+"-"+request.AssetDigest.Hex,
	)
	extractedDir := filepath.Join(storePath, "extracted")
	storeParent := filepath.Dir(storePath)
	if err := os.MkdirAll(storeParent, 0o755); err != nil {
		return app.StoreLayout{}, fmt.Errorf("create store parent: %w", err)
	}
	if err := os.Mkdir(storePath, 0o755); err != nil {
		if os.IsExist(err) {
			return app.StoreLayout{}, fmt.Errorf("store path %s already exists", storePath)
		}
		return app.StoreLayout{}, fmt.Errorf("create store path: %w", err)
	}
	if err := os.Mkdir(extractedDir, 0o755); err != nil {
		_ = os.RemoveAll(storePath)
		if os.IsExist(err) {
			return app.StoreLayout{}, fmt.Errorf("extracted store directory %s already exists", extractedDir)
		}
		return app.StoreLayout{}, fmt.Errorf("create extraction directory: %w", err)
	}

	storedArtifact := filepath.Join(storePath, "artifact")
	if err := copyFileExclusive(request.ArtifactPath, storedArtifact, 0o600); err != nil {
		_ = os.RemoveAll(storePath)
		return app.StoreLayout{}, err
	}

	return app.StoreLayout{
		StorePath:    storePath,
		ArtifactPath: storedArtifact,
		ExtractedDir: extractedDir,
	}, nil
}

// RemoveStoreLayout removes a store layout created for an incomplete install.
func (Installer) RemoveStoreLayout(ctx context.Context, layout app.StoreLayout) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	storePath := strings.TrimSpace(layout.StorePath)
	if storePath == "" {
		return fmt.Errorf("store path must be set")
	}
	clean := filepath.Clean(storePath)
	if clean == "." || clean == string(os.PathSeparator) {
		return fmt.Errorf("refusing to remove unsafe store path %s", storePath)
	}
	if err := os.RemoveAll(clean); err != nil {
		return fmt.Errorf("remove incomplete store layout: %w", err)
	}
	return nil
}

// ValidateInstalledStore checks whether a recorded store path can be removed.
func (Installer) ValidateInstalledStore(ctx context.Context, request app.RemoveInstalledStoreRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, rel, err := openInstalledStoreRoot(request)
	if err != nil {
		return err
	}
	defer root.Close()
	return rejectSymlinkComponents(root, rel)
}

// RemoveInstalledStore removes a recorded store path under the configured store root.
func (Installer) RemoveInstalledStore(ctx context.Context, request app.RemoveInstalledStoreRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, rel, err := openInstalledStoreRoot(request)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := rejectSymlinkComponents(root, rel); err != nil {
		return err
	}
	if err := root.RemoveAll(rel); err != nil {
		return fmt.Errorf("remove installed store path: %w", err)
	}
	return nil
}

// LinkBinaries links extracted binaries into the managed bin directory.
func (Installer) LinkBinaries(ctx context.Context, request app.LinkBinariesRequest) ([]app.InstalledBinary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.BinDir) == "" {
		return nil, fmt.Errorf("bin directory must be set")
	}
	if len(request.Binaries) == 0 {
		return nil, fmt.Errorf("at least one binary must be linked")
	}
	if err := os.MkdirAll(request.BinDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bin directory: %w", err)
	}

	created := make([]app.InstalledBinary, 0, len(request.Binaries))
	cleanup := func() {
		_ = removeBinaryLinks(context.WithoutCancel(ctx), created)
	}
	installed := make([]app.InstalledBinary, 0, len(request.Binaries))
	for _, binary := range request.Binaries {
		name, err := cleanPathSegment("binary name", binary.Name)
		if err != nil {
			cleanup()
			return nil, err
		}
		if strings.TrimSpace(binary.Path) == "" {
			cleanup()
			return nil, fmt.Errorf("binary %q target path must be set", binary.Name)
		}
		linkPath := filepath.Join(request.BinDir, name)
		if err := os.Symlink(binary.Path, linkPath); err != nil {
			cleanup()
			if os.IsExist(err) {
				return nil, fmt.Errorf("binary link %s already exists", linkPath)
			}
			return nil, fmt.Errorf("link binary %s: %w", name, err)
		}
		installedBinary := app.InstalledBinary{
			Name:       name,
			LinkPath:   linkPath,
			TargetPath: binary.Path,
		}
		created = append(created, installedBinary)
		installed = append(installed, installedBinary)
	}
	return installed, nil
}

// RemoveBinaryLinks removes managed binary links created for an incomplete install.
func (Installer) RemoveBinaryLinks(ctx context.Context, binaries []app.InstalledBinary) error {
	return removeBinaryLinks(ctx, binaries)
}

func removeBinaryLinks(ctx context.Context, binaries []app.InstalledBinary) error {
	var errs []error
	for _, binary := range binaries {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if strings.TrimSpace(binary.LinkPath) == "" {
			continue
		}
		info, err := os.Lstat(binary.LinkPath)
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
		target, err := os.Readlink(binary.LinkPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("read binary link %s: %w", binary.LinkPath, err))
			continue
		}
		if target != binary.TargetPath {
			errs = append(errs, fmt.Errorf("refusing to remove binary link %s with unexpected target %s", binary.LinkPath, target))
			continue
		}
		if err := os.Remove(binary.LinkPath); err != nil {
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
			return fmt.Errorf("store artifact %s already exists", destination)
		}
		return fmt.Errorf("create store artifact: %w", err)
	}
	removeOutput := true
	defer func() {
		if removeOutput {
			_ = os.Remove(destination)
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

func openInstalledStoreRoot(request app.RemoveInstalledStoreRequest) (*os.Root, string, error) {
	storeRoot, err := cleanStoreRoot(request.StoreRoot)
	if err != nil {
		return nil, "", err
	}
	rel, err := cleanStoreRelativePath(storeRoot, request.StorePath)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(storeRoot)
	if err != nil {
		return nil, "", fmt.Errorf("open store root: %w", err)
	}
	return root, rel, nil
}

func cleanStoreRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("store root must be set")
	}
	root, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", fmt.Errorf("resolve store root: %w", err)
	}
	if root == string(os.PathSeparator) {
		return "", fmt.Errorf("refusing to use unsafe store root %s", value)
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
