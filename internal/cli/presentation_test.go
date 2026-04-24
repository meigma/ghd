package cli

import (
	"bytes"
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

func TestFormatRowsEscapesTerminalControlCharacters(t *testing.T) {
	got := formatRows([]uiRow{
		{label: "Asset", value: "foo\n\x1b[31mbar"},
	}, 6)

	assert.Equal(t, `Asset: foo\n\x1b[31mbar`, got)
}

func TestWriteTrustedRootNoticeEscapesPath(t *testing.T) {
	var buf bytes.Buffer

	writeTrustedRootNotice(&buf, "/tmp/root\ntrusted.json")

	assert.Equal(t, "using custom Sigstore trust root /tmp/root\\ntrusted.json\n", buf.String())
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

func TestTransientStatusLineShowAndClear(t *testing.T) {
	var buf bytes.Buffer

	status := newTransientStatusLine(&buf, false)
	status.Show("Loading indexed packages")
	status.Clear()

	assert.Equal(t, "\r\033[KLoading indexed packages\r\033[K", buf.String())
}
