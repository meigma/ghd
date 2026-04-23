package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/meigma/ghd/internal/state"
)

// InstalledPackagesDependencies contains the ports needed by InstalledPackages.
type InstalledPackagesDependencies struct {
	// StateStore persists active installed package records.
	StateStore InstalledStateStore
}

// InstalledPackages implements installed package queries.
type InstalledPackages struct {
	state InstalledStateStore
}

// InstalledListRequest describes an installed package list request.
type InstalledListRequest struct {
	// StateDir stores active installed package state.
	StateDir string
}

// InstalledListResult contains active installed package records.
type InstalledListResult struct {
	// Records are active installed package records.
	Records []state.Record
}

// NewInstalledPackages creates an installed package query use case.
func NewInstalledPackages(deps InstalledPackagesDependencies) (*InstalledPackages, error) {
	if deps.StateStore == nil {
		return nil, fmt.Errorf("installed state store must be set")
	}
	return &InstalledPackages{state: deps.StateStore}, nil
}

// ListInstalled returns active installed package records.
func (p *InstalledPackages) ListInstalled(ctx context.Context, request InstalledListRequest) (InstalledListResult, error) {
	if strings.TrimSpace(request.StateDir) == "" {
		return InstalledListResult{}, fmt.Errorf("state directory must be set")
	}
	index, err := p.state.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return InstalledListResult{}, err
	}
	return InstalledListResult{Records: index.Normalize().Records}, nil
}
