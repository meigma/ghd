package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitHubRateLimitStatus describes GitHub API rate-limit readiness.
type GitHubRateLimitStatus struct {
	// CoreLimit is the GitHub core API quota limit.
	CoreLimit int
	// CoreRemaining is the remaining GitHub core API quota.
	CoreRemaining int
	// CoreUsed is the used GitHub core API quota.
	CoreUsed int
}

// GitHubDoctorChecker checks GitHub API readiness.
type GitHubDoctorChecker interface {
	// CheckRateLimit returns the current GitHub core API rate-limit status.
	CheckRateLimit(ctx context.Context) (GitHubRateLimitStatus, error)
}

// TrustedRootChecker validates a custom trusted_root.json path.
type TrustedRootChecker interface {
	// ValidateTrustedRoot checks that a custom trusted root exists and parses successfully.
	ValidateTrustedRoot(ctx context.Context, path string) error
}

// EnvironmentDoctorDependencies contains the ports needed by EnvironmentDoctor.
type EnvironmentDoctorDependencies struct {
	// GitHub checks GitHub API readiness.
	GitHub GitHubDoctorChecker
	// TrustedRoot validates custom trusted_root.json files.
	TrustedRoot TrustedRootChecker
}

// DoctorRequest describes one environment diagnostics run.
type DoctorRequest struct {
	// GitHubToken is the optional configured GitHub token.
	GitHubToken string
	// TrustedRootPath optionally points to a custom trusted_root.json file.
	TrustedRootPath string
	// IndexDir is the local repository index directory.
	IndexDir string
	// StoreDir is the managed package store directory.
	StoreDir string
	// StateDir is the local installed package state directory.
	StateDir string
	// BinDir is the managed binary link directory.
	BinDir string
	// PathEnv is the current PATH value.
	PathEnv string
}

// DoctorStatus is one environment check status.
type DoctorStatus string

const (
	// DoctorStatusPass reports a ready check.
	DoctorStatusPass DoctorStatus = "pass"
	// DoctorStatusWarn reports a non-blocking issue.
	DoctorStatusWarn DoctorStatus = "warn"
	// DoctorStatusFail reports a blocking issue.
	DoctorStatusFail DoctorStatus = "fail"
)

// DoctorResult is one environment check result.
type DoctorResult struct {
	// ID is the stable check identifier.
	ID string
	// Status is the check outcome.
	Status DoctorStatus
	// Message is the human-readable check result.
	Message string
}

// DoctorFailureError reports one or more failing doctor checks.
type DoctorFailureError struct {
	// Failed is the number of failing checks.
	Failed int
}

// Error describes the aggregated doctor failure.
func (e DoctorFailureError) Error() string {
	if e.Failed == 1 {
		return "doctor found 1 failing check"
	}
	return fmt.Sprintf("doctor found %d failing checks", e.Failed)
}

// EnvironmentDoctor implements environment diagnostics.
type EnvironmentDoctor struct {
	github      GitHubDoctorChecker
	trustedRoot TrustedRootChecker
}

// NewEnvironmentDoctor creates an environment diagnostics use case.
func NewEnvironmentDoctor(deps EnvironmentDoctorDependencies) (*EnvironmentDoctor, error) {
	if deps.GitHub == nil {
		return nil, errors.New("GitHub doctor checker must be set")
	}
	if deps.TrustedRoot == nil {
		return nil, errors.New("trusted root checker must be set")
	}
	return &EnvironmentDoctor{
		github:      deps.GitHub,
		trustedRoot: deps.TrustedRoot,
	}, nil
}

// Doctor runs environment diagnostics in stable output order.
func (d *EnvironmentDoctor) Doctor(ctx context.Context, request DoctorRequest) ([]DoctorResult, error) {
	results := []DoctorResult{
		doctorBinDirOnPath(request.PathEnv, request.BinDir),
		doctorDirectoryCheck("index-dir", request.IndexDir),
		doctorDirectoryCheck("store-dir", request.StoreDir),
		doctorDirectoryCheck("state-dir", request.StateDir),
		doctorDirectoryCheck("bin-dir", request.BinDir),
		d.doctorTrustedRoot(ctx, request.TrustedRootPath),
		d.doctorGitHubAPI(ctx, request.GitHubToken),
	}
	failed := 0
	for _, result := range results {
		if result.Status == DoctorStatusFail {
			failed++
		}
	}
	if failed != 0 {
		return results, DoctorFailureError{Failed: failed}
	}
	return results, nil
}

func doctorBinDirOnPath(pathEnv string, binDir string) DoctorResult {
	binRoot, err := doctorDirectoryRoot(binDir)
	if err != nil {
		return DoctorResult{ID: "bin-dir-on-path", Status: DoctorStatusFail, Message: err.Error()}
	}
	for _, entry := range filepath.SplitList(pathEnv) {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		candidate, err := filepath.Abs(filepath.Clean(entry))
		if err != nil {
			continue
		}
		if candidate == binRoot {
			return DoctorResult{
				ID:      "bin-dir-on-path",
				Status:  DoctorStatusPass,
				Message: fmt.Sprintf("managed bin directory %s is on PATH", binRoot),
			}
		}
	}
	return DoctorResult{
		ID:      "bin-dir-on-path",
		Status:  DoctorStatusWarn,
		Message: fmt.Sprintf("managed bin directory %s is not on PATH", binRoot),
	}
}

func doctorDirectoryCheck(id string, directory string) DoctorResult {
	root, err := doctorDirectoryRoot(directory)
	if err != nil {
		return DoctorResult{ID: id, Status: DoctorStatusFail, Message: err.Error()}
	}
	info, err := os.Stat(root)
	switch {
	case err == nil:
		if !info.IsDir() {
			return DoctorResult{
				ID:      id,
				Status:  DoctorStatusFail,
				Message: fmt.Sprintf("%s path %s exists but is not a directory", id, root),
			}
		}
		file, createErr := os.CreateTemp(root, ".ghd-doctor-*")
		if createErr != nil {
			return DoctorResult{
				ID:      id,
				Status:  DoctorStatusFail,
				Message: fmt.Sprintf("%s directory %s is not writable: %v", id, root, createErr),
			}
		}
		tempPath := file.Name()
		_ = file.Close()
		_ = os.Remove(tempPath)
		return DoctorResult{
			ID:      id,
			Status:  DoctorStatusPass,
			Message: fmt.Sprintf("%s directory %s is writable", id, root),
		}
	case !os.IsNotExist(err):
		return DoctorResult{
			ID:      id,
			Status:  DoctorStatusFail,
			Message: fmt.Sprintf("inspect %s path %s: %v", id, root, err),
		}
	}

	parent, err := nearestExistingParent(root)
	if err != nil {
		return DoctorResult{
			ID:      id,
			Status:  DoctorStatusFail,
			Message: fmt.Sprintf("resolve parent for %s path %s: %v", id, root, err),
		}
	}
	file, err := os.CreateTemp(parent, ".ghd-doctor-*")
	if err != nil {
		return DoctorResult{
			ID:      id,
			Status:  DoctorStatusFail,
			Message: fmt.Sprintf("%s directory %s cannot be created from parent %s: %v", id, root, parent, err),
		}
	}
	tempPath := file.Name()
	_ = file.Close()
	_ = os.Remove(tempPath)
	return DoctorResult{
		ID:      id,
		Status:  DoctorStatusPass,
		Message: fmt.Sprintf("%s directory %s does not exist but parent %s is writable", id, root, parent),
	}
}

func doctorDirectoryRoot(directory string) (string, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return "", errors.New("directory must be set")
	}
	root, err := filepath.Abs(filepath.Clean(directory))
	if err != nil {
		return "", fmt.Errorf("resolve directory %s: %w", directory, err)
	}
	if root == string(os.PathSeparator) {
		return "", fmt.Errorf("refusing to use unsafe directory %s", directory)
	}
	return root, nil
}

func nearestExistingParent(path string) (string, error) {
	current := path
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("no existing parent found")
		}
		info, err := os.Stat(parent)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("parent path %s is not a directory", parent)
			}
			return parent, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		current = parent
	}
}

func (d *EnvironmentDoctor) doctorTrustedRoot(ctx context.Context, trustedRootPath string) DoctorResult {
	if strings.TrimSpace(trustedRootPath) == "" {
		return DoctorResult{
			ID:      "trusted-root",
			Status:  DoctorStatusPass,
			Message: "using built-in Sigstore trust roots",
		}
	}
	if err := d.trustedRoot.ValidateTrustedRoot(ctx, trustedRootPath); err != nil {
		return DoctorResult{
			ID:      "trusted-root",
			Status:  DoctorStatusFail,
			Message: fmt.Sprintf("trusted root %s is invalid: %v", trustedRootPath, err),
		}
	}
	return DoctorResult{
		ID:      "trusted-root",
		Status:  DoctorStatusPass,
		Message: fmt.Sprintf("trusted root %s parsed successfully", trustedRootPath),
	}
}

func (d *EnvironmentDoctor) doctorGitHubAPI(ctx context.Context, githubToken string) DoctorResult {
	status, err := d.github.CheckRateLimit(ctx)
	if err != nil {
		return DoctorResult{
			ID:      "github-api",
			Status:  DoctorStatusFail,
			Message: fmt.Sprintf("GitHub API check failed: %v", err),
		}
	}
	if status.CoreRemaining <= 0 {
		return DoctorResult{
			ID:      "github-api",
			Status:  DoctorStatusFail,
			Message: fmt.Sprintf("GitHub API core rate limit exhausted (0/%d remaining)", status.CoreLimit),
		}
	}
	if strings.TrimSpace(githubToken) == "" {
		return DoctorResult{
			ID:     "github-api",
			Status: DoctorStatusWarn,
			Message: fmt.Sprintf(
				"GitHub token is not set; unauthenticated core rate limit remaining %d/%d",
				status.CoreRemaining,
				status.CoreLimit,
			),
		}
	}
	return DoctorResult{
		ID:     "github-api",
		Status: DoctorStatusPass,
		Message: fmt.Sprintf(
			"GitHub API reachable; core rate limit remaining %d/%d",
			status.CoreRemaining,
			status.CoreLimit,
		),
	}
}
