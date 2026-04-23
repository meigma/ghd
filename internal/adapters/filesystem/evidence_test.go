package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/verification"
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

func TestEvidenceWriterReadsVerificationJSON(t *testing.T) {
	outputDir := t.TempDir()
	writer := NewEvidenceWriter()
	path, err := writer.WriteVerificationEvidence(context.Background(), outputDir, app.VerificationRecord{
		SchemaVersion: 1,
		Repository:    "owner/repo",
		Package:       "foo",
		Version:       "1.2.3",
		Tag:           "v1.2.3",
		Asset:         "foo.tar.gz",
		Evidence:      verificationEvidenceForTest(t),
	})
	require.NoError(t, err)

	record, err := writer.ReadVerificationRecord(context.Background(), path)

	require.NoError(t, err)
	assert.Equal(t, "owner/repo", record.Repository)
	assert.Equal(t, "foo", record.Package)
	assert.Equal(t, "v1.2.3", record.Tag)
}

func verificationEvidenceForTest(t *testing.T) verification.Evidence {
	t.Helper()
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	require.NoError(t, err)
	return verification.Evidence{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		Tag:         "v1.2.3",
		AssetDigest: digest,
		ProvenanceAttestation: verification.AttestationEvidence{
			SignerWorkflow: "owner/repo/.github/workflows/release.yml",
		},
	}
}
