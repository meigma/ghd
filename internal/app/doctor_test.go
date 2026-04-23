package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentDoctor(t *testing.T) {
	t.Run("bin dir missing from PATH is warning", func(t *testing.T) {
		tc := newEnvironmentDoctorTestContext(t)
		tc.request.PathEnv = filepath.Join(t.TempDir(), "other-bin")

		results, err := tc.subject.Doctor(context.Background(), tc.request)

		require.NoError(t, err)
		assert.Equal(t, DoctorStatusWarn, doctorResultByID(t, results, "bin-dir-on-path").Status)
	})

	t.Run("invalid custom trusted root fails", func(t *testing.T) {
		tc := newEnvironmentDoctorTestContext(t)
		tc.trustedRoot.err = fmt.Errorf("parse trusted root: bad root")
		tc.request.TrustedRootPath = filepath.Join(t.TempDir(), "invalid-root.json")

		results, err := tc.subject.Doctor(context.Background(), tc.request)

		require.Error(t, err)
		assert.Equal(t, DoctorStatusFail, doctorResultByID(t, results, "trusted-root").Status)
	})

	t.Run("missing token is warning", func(t *testing.T) {
		tc := newEnvironmentDoctorTestContext(t)
		tc.request.GitHubToken = ""

		results, err := tc.subject.Doctor(context.Background(), tc.request)

		require.NoError(t, err)
		assert.Equal(t, DoctorStatusWarn, doctorResultByID(t, results, "github-api").Status)
	})

	t.Run("GitHub API failure fails", func(t *testing.T) {
		tc := newEnvironmentDoctorTestContext(t)
		tc.github.err = fmt.Errorf("GET /rate_limit returned HTTP 401")

		results, err := tc.subject.Doctor(context.Background(), tc.request)

		require.Error(t, err)
		assert.Equal(t, DoctorStatusFail, doctorResultByID(t, results, "github-api").Status)
	})

	t.Run("exhausted rate limit fails", func(t *testing.T) {
		tc := newEnvironmentDoctorTestContext(t)
		tc.github.status = GitHubRateLimitStatus{CoreLimit: 60, CoreRemaining: 0}

		results, err := tc.subject.Doctor(context.Background(), tc.request)

		require.Error(t, err)
		assert.Equal(t, DoctorStatusFail, doctorResultByID(t, results, "github-api").Status)
	})

	t.Run("directory collision with file fails", func(t *testing.T) {
		tc := newEnvironmentDoctorTestContext(t)
		filePath := filepath.Join(t.TempDir(), "store-file")
		require.NoError(t, os.WriteFile(filePath, []byte("not a directory"), 0o644))
		tc.request.StoreDir = filePath

		results, err := tc.subject.Doctor(context.Background(), tc.request)

		require.Error(t, err)
		assert.Equal(t, DoctorStatusFail, doctorResultByID(t, results, "store-dir").Status)
	})

	t.Run("missing but creatable directories pass", func(t *testing.T) {
		tc := newEnvironmentDoctorTestContext(t)
		root := t.TempDir()
		tc.request.IndexDir = filepath.Join(root, "index", "nested")
		tc.request.StoreDir = filepath.Join(root, "store", "nested")
		tc.request.StateDir = filepath.Join(root, "state", "nested")
		tc.request.BinDir = filepath.Join(root, "bin", "nested")
		tc.request.PathEnv = filepath.Join(t.TempDir(), "other-bin")

		results, err := tc.subject.Doctor(context.Background(), tc.request)

		require.NoError(t, err)
		assert.Equal(t, DoctorStatusPass, doctorResultByID(t, results, "index-dir").Status)
		assert.Equal(t, DoctorStatusPass, doctorResultByID(t, results, "store-dir").Status)
		assert.Equal(t, DoctorStatusPass, doctorResultByID(t, results, "state-dir").Status)
		assert.Equal(t, DoctorStatusPass, doctorResultByID(t, results, "bin-dir").Status)
	})
}

type environmentDoctorTestContext struct {
	subject     *EnvironmentDoctor
	github      *fakeDoctorGitHubChecker
	trustedRoot *fakeDoctorTrustedRootChecker
	request     DoctorRequest
}

func newEnvironmentDoctorTestContext(t *testing.T) *environmentDoctorTestContext {
	t.Helper()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	github := &fakeDoctorGitHubChecker{
		status: GitHubRateLimitStatus{CoreLimit: 5000, CoreRemaining: 4999, CoreUsed: 1},
	}
	trustedRoot := &fakeDoctorTrustedRootChecker{}
	subject, err := NewEnvironmentDoctor(EnvironmentDoctorDependencies{
		GitHub:      github,
		TrustedRoot: trustedRoot,
	})
	require.NoError(t, err)

	return &environmentDoctorTestContext{
		subject:     subject,
		github:      github,
		trustedRoot: trustedRoot,
		request: DoctorRequest{
			GitHubToken: "token-123",
			IndexDir:    filepath.Join(root, "index"),
			StoreDir:    filepath.Join(root, "store"),
			StateDir:    filepath.Join(root, "state"),
			BinDir:      binDir,
			PathEnv:     binDir,
		},
	}
}

type fakeDoctorGitHubChecker struct {
	status GitHubRateLimitStatus
	err    error
}

func (f *fakeDoctorGitHubChecker) CheckRateLimit(context.Context) (GitHubRateLimitStatus, error) {
	if f.err != nil {
		return GitHubRateLimitStatus{}, f.err
	}
	return f.status, nil
}

type fakeDoctorTrustedRootChecker struct {
	err error
}

func (f *fakeDoctorTrustedRootChecker) ValidateTrustedRoot(context.Context, string) error {
	return f.err
}

func doctorResultByID(t *testing.T, results []DoctorResult, id string) DoctorResult {
	t.Helper()
	for _, result := range results {
		if result.ID == id {
			return result
		}
	}
	t.Fatalf("missing doctor result %s", id)
	return DoctorResult{}
}
