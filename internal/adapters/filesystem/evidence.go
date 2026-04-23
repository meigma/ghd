package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/app"
)

// EvidenceWriter writes verification records as JSON files.
type EvidenceWriter struct{}

// NewEvidenceWriter creates a filesystem evidence writer.
func NewEvidenceWriter() EvidenceWriter {
	return EvidenceWriter{}
}

// ReadVerificationRecord reads one persisted verification.json record.
func (EvidenceWriter) ReadVerificationRecord(ctx context.Context, path string) (app.VerificationRecord, error) {
	if err := ctx.Err(); err != nil {
		return app.VerificationRecord{}, err
	}
	if strings.TrimSpace(path) == "" {
		return app.VerificationRecord{}, fmt.Errorf("verification path must be set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return app.VerificationRecord{}, fmt.Errorf("read verification record: %w", err)
	}
	var record app.VerificationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return app.VerificationRecord{}, fmt.Errorf("decode verification record: %w", err)
	}
	if err := record.Validate(); err != nil {
		return app.VerificationRecord{}, err
	}
	return record, nil
}

// WriteVerificationEvidence writes verification.json into outputDir.
func (EvidenceWriter) WriteVerificationEvidence(ctx context.Context, outputDir string, record app.VerificationRecord) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if outputDir == "" {
		return "", fmt.Errorf("output directory must be set")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode verification evidence: %w", err)
	}
	data = append(data, '\n')

	finalPath := filepath.Join(outputDir, "verification.json")
	temp, err := os.CreateTemp(outputDir, ".verification-*.json.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary verification evidence: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write temporary verification evidence: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temporary verification evidence: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", fmt.Errorf("commit verification evidence: %w", err)
	}
	removeTemp = false
	return finalPath, nil
}
