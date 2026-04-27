package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	originalVersion, originalCommit, originalDate := version, commit, date
	version, commit, date = "1.2.3", "abc123", "2026-04-25T13:00:00Z"
	t.Cleanup(func() {
		version, commit, date = originalVersion, originalCommit, originalDate
	})

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "default command prints hello world",
			args:       nil,
			wantCode:   0,
			wantStdout: "hello from ghd-example\n",
		},
		{
			name:       "version command prints build metadata",
			args:       []string{"version"},
			wantCode:   0,
			wantStdout: "ghd-example 1.2.3 (abc123) built 2026-04-25T13:00:00Z\n",
		},
		{
			name:       "version flag prints build metadata",
			args:       []string{"--version"},
			wantCode:   0,
			wantStdout: "ghd-example 1.2.3 (abc123) built 2026-04-25T13:00:00Z\n",
		},
		{
			name:       "help prints usage",
			args:       []string{"--help"},
			wantCode:   0,
			wantStdout: "Usage: ghd-example [version|--version]\n",
		},
		{
			name:       "version rejects extra arguments",
			args:       []string{"version", "extra"},
			wantCode:   2,
			wantStderr: "version accepts no arguments\n",
		},
		{
			name:       "unknown command fails",
			args:       []string{"nope"},
			wantCode:   2,
			wantStderr: "unknown command \"nope\"\nUsage: ghd-example [version|--version]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout strings.Builder
			var stderr strings.Builder

			gotCode := run(tt.args, &stdout, &stderr)

			assert.Equal(t, tt.wantCode, gotCode)
			assert.Equal(t, tt.wantStdout, stdout.String())
			assert.Equal(t, tt.wantStderr, stderr.String())
		})
	}
}
