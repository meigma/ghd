package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
)

func TestEvidenceWriterWritesVerificationJSON(t *testing.T) {
	outputDir := t.TempDir()
	writer := NewEvidenceWriter()

	path, err := writer.WriteVerificationEvidence(context.Background(), outputDir, app.VerificationRecord{
		SchemaVersion: 1,
		Repository:    "owner/repo",
		Package:       "foo",
		Version:       "1.2.3",
		Tag:           "v1.2.3",
		Asset:         "foo.tar.gz",
	})

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(outputDir, "verification.json"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var record app.VerificationRecord
	require.NoError(t, json.Unmarshal(data, &record))
	assert.Equal(t, "owner/repo", record.Repository)
	assert.Equal(t, "foo.tar.gz", record.Asset)
}
