package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatRowsSkipsEmptyValuesAndAlignsLabels(t *testing.T) {
	got := formatRows([]uiRow{
		{label: "From", value: "owner/repo"},
		{label: "Empty"},
		{label: "Verified", value: "GitHub release + SLSA provenance"},
	}, 9)

	assert.Equal(t, "From:     owner/repo\nVerified: GitHub release + SLSA provenance", got)
}

func TestRenderProgressBarClampsRatio(t *testing.T) {
	styles := newUIStyles(false)

	assert.Equal(t, "[----------]", renderProgressBar(-0.5, 10, styles))
	assert.Equal(t, "[##########]", renderProgressBar(2, 10, styles))
}

func TestFormatByteCount(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "negative", bytes: -1, want: "0 B"},
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "kibibytes", bytes: 1536, want: "1.5 KiB"},
		{name: "mebibytes", bytes: 2 * 1024 * 1024, want: "2.0 MiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatByteCount(tt.bytes))
		})
	}
}
