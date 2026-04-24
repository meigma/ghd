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
	// RemoveManagedInstall removes managed binaries and store contents for one install.
	RemoveManagedInstall(ctx context.Context, request RemoveManagedInstallRequest) error
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

// UninstallProgressStage identifies one user-visible uninstall step.
type UninstallProgressStage string

const (
	// UninstallProgressLoadingState means ghd is loading installed package state.
	UninstallProgressLoadingState UninstallProgressStage = "loading-state"
	// UninstallProgressRemovingManaged means ghd is removing managed binaries and store data.
	UninstallProgressRemovingManaged UninstallProgressStage = "removing-managed"
	// UninstallProgressRemovingState means ghd is updating installed package state.
	UninstallProgressRemovingState UninstallProgressStage = "removing-state"
)

// UninstallProgress describes user-visible uninstall progress.
type UninstallProgress struct {
	// Stage identifies the current lifecycle step.
	Stage UninstallProgressStage
	// Message is a short user-facing description of the current step.
	Message string
}

// UninstallProgressFunc receives user-visible uninstall progress.
type UninstallProgressFunc func(UninstallProgress)

// UninstallRequest describes one uninstall request.
type UninstallRequest struct {
	// Target is a package name, binary name, or owner/repo/package.
	Target string
	// StoreDir is the root of ghd's managed package store.
	StoreDir string
	// BinDir is the managed binary link directory.
	BinDir string
	// StateDir stores active installed package state.
	StateDir string
	// Progress receives user-visible uninstall progress. Nil disables progress reports.
	Progress UninstallProgressFunc
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
func (u *PackageUninstaller) Uninstall(ctx context.Context, request UninstallRequest) (state.Record, error) {
	if err := request.validate(); err != nil {
		return state.Record{}, err
	}
	request.report(UninstallProgressLoadingState, "Loading installed packages")
	index, err := u.state.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return state.Record{}, err
	}
	record, err := index.ResolveTarget(request.Target)
	if err != nil {
		return state.Record{}, err
	}
	request.report(UninstallProgressRemovingManaged, "Removing managed files")
	if err := u.files.RemoveManagedInstall(ctx, RemoveManagedInstallRequest{
		StoreRoot: request.StoreDir,
		BinRoot:   request.BinDir,
		StorePath: record.StorePath,
		Binaries:  installedBinaries(record.Binaries),
	}); err != nil {
		return state.Record{}, err
	}
	request.report(UninstallProgressRemovingState, "Updating installed state")
	if _, err := u.state.RemoveInstalledRecord(ctx, request.StateDir, record.Repository, record.Package); err != nil {
		return state.Record{}, err
	}
	return record, nil
}

func (r UninstallRequest) validate() error {
	if strings.TrimSpace(r.Target) == "" {
		return fmt.Errorf("uninstall target must be set")
	}
	if strings.TrimSpace(r.StoreDir) == "" {
		return fmt.Errorf("store directory must be set")
	}
	if strings.TrimSpace(r.BinDir) == "" {
		return fmt.Errorf("bin directory must be set")
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

func (r UninstallRequest) report(stage UninstallProgressStage, message string) {
	if r.Progress == nil {
		return
	}
	r.Progress(UninstallProgress{
		Stage:   stage,
		Message: message,
	})
}
