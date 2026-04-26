package app

import (
	"context"
	"errors"
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

// NewInstalledPackages creates an installed package query use case.
func NewInstalledPackages(deps InstalledPackagesDependencies) (*InstalledPackages, error) {
	if deps.StateStore == nil {
		return nil, errors.New("installed state store must be set")
	}
	return &InstalledPackages{state: deps.StateStore}, nil
}

// ListInstalled returns active installed package records.
func (p *InstalledPackages) ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("state directory must be set")
	}
	index, err := p.state.LoadInstalledState(ctx, stateDir)
	if err != nil {
		return nil, err
	}
	return index.Normalize().Records, nil
}
