package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/spf13/viper"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"ghd": runTestCommand,
	}))
}

func TestCLI(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
	})
}

func runTestCommand() int {
	vp := viper.New()
	root := NewRootCommand(Options{
		Out:   os.Stdout,
		Err:   os.Stderr,
		Viper: vp,
		RuntimeFactory: func(context.Context, config.Config) (DownloadUseCase, error) {
			return testDownloader{}, nil
		},
	})
	root.SetArgs(os.Args[1:])
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

type testDownloader struct{}

func (testDownloader) Download(_ context.Context, request app.VerifiedDownloadRequest) (app.VerifiedDownloadResult, error) {
	artifactPath := filepath.Join(request.OutputDir, "artifact.tar.gz")
	evidencePath := filepath.Join(request.OutputDir, "verification.json")
	if err := os.MkdirAll(request.OutputDir, 0o755); err != nil {
		return app.VerifiedDownloadResult{}, err
	}
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o600); err != nil {
		return app.VerifiedDownloadResult{}, err
	}
	if err := os.WriteFile(evidencePath, []byte("{}\n"), 0o600); err != nil {
		return app.VerifiedDownloadResult{}, err
	}
	return app.VerifiedDownloadResult{
		Repository:   request.Repository,
		PackageName:  request.PackageName,
		Version:      request.Version,
		ArtifactPath: artifactPath,
		EvidencePath: evidencePath,
	}, nil
}
