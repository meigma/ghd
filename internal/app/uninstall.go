package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/meigma/ghd/internal/state"
)

// InstalledStateRemoveStore persists active installed package removal.
type InstalledStateRemoveStore interface {
	// LoadInstalledState reads active installed package state from stateDir.
	LoadInstalledState(ctx context.Context, stateDir string) (state.Index, error)
	// RemoveInstalledRecord removes an active installed package record from stateDir.
	RemoveInstalledRecord(ctx context.Context, stateDir string, repository string, packageName string) (state.Index, error)
}

// UninstallFileSystem owns uninstall-time filesystem cleanup.
type UninstallFileSystem interface {
	// RemoveBinaryLinks removes managed binary links created by an install.
	RemoveBinaryLinks(ctx context.Context, binaries []InstalledBinary) error
	// RemoveInstalledStore removes one recorded store path under StoreRoot.
	RemoveInstalledStore(ctx context.Context, request RemoveInstalledStoreRequest) error
}

// PackageUninstallerDependencies contains the ports needed by PackageUninstaller.
type PackageUninstallerDependencies struct {
	// StateStore persists active installed package records.
	StateStore InstalledStateRemoveStore
	// FileSystem owns uninstall filesystem cleanup.
	FileSystem UninstallFileSystem
}

// PackageUninstaller implements package uninstall.
type PackageUninstaller struct {
	state InstalledStateRemoveStore
	files UninstallFileSystem
}

// UninstallRequest describes one uninstall request.
type UninstallRequest struct {
	// Target is a package name, binary name, or owner/repo/package.
	Target string
	// StoreDir is the root of ghd's managed package store.
	StoreDir string
	// StateDir stores active installed package state.
	StateDir string
}

// UninstallResult describes a completed uninstall.
type UninstallResult struct {
	// Record is the removed active install record.
	Record state.Record
}

// RemoveInstalledStoreRequest describes one recorded store path to remove.
type RemoveInstalledStoreRequest struct {
	// StoreRoot is the configured managed store root.
	StoreRoot string
	// StorePath is the recorded digest-keyed store directory.
	StorePath string
}

// NewPackageUninstaller creates an uninstall use case.
func NewPackageUninstaller(deps PackageUninstallerDependencies) (*PackageUninstaller, error) {
	if deps.StateStore == nil {
		return nil, fmt.Errorf("installed state store must be set")
	}
	if deps.FileSystem == nil {
		return nil, fmt.Errorf("uninstall filesystem must be set")
	}
	return &PackageUninstaller{state: deps.StateStore, files: deps.FileSystem}, nil
}

// Uninstall removes one active package install.
func (u *PackageUninstaller) Uninstall(ctx context.Context, request UninstallRequest) (UninstallResult, error) {
	if err := request.validate(); err != nil {
		return UninstallResult{}, err
	}
	index, err := u.state.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return UninstallResult{}, err
	}
	record, err := index.ResolveTarget(request.Target)
	if err != nil {
		return UninstallResult{}, err
	}
	links := installedBinaries(record.Binaries)
	if err := u.files.RemoveBinaryLinks(ctx, links); err != nil {
		return UninstallResult{}, err
	}
	if _, err := u.state.RemoveInstalledRecord(ctx, request.StateDir, record.Repository, record.Package); err != nil {
		return UninstallResult{}, err
	}
	if err := u.files.RemoveInstalledStore(ctx, RemoveInstalledStoreRequest{
		StoreRoot: request.StoreDir,
		StorePath: record.StorePath,
	}); err != nil {
		return UninstallResult{}, err
	}
	return UninstallResult{Record: record}, nil
}

func (r UninstallRequest) validate() error {
	if strings.TrimSpace(r.Target) == "" {
		return fmt.Errorf("uninstall target must be set")
	}
	if strings.TrimSpace(r.StoreDir) == "" {
		return fmt.Errorf("store directory must be set")
	}
	if strings.TrimSpace(r.StateDir) == "" {
		return fmt.Errorf("state directory must be set")
	}
	return nil
}

func installedBinaries(binaries []state.Binary) []InstalledBinary {
	records := make([]InstalledBinary, 0, len(binaries))
	for _, binary := range binaries {
		records = append(records, InstalledBinary{
			Name:       binary.Name,
			LinkPath:   binary.LinkPath,
			TargetPath: binary.TargetPath,
		})
	}
	return records
}
