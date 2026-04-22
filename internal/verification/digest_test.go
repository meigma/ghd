package verification

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSHA256FileDigesterDigestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o600))

	digest, err := SHA256FileDigester{}.DigestFile(path)

	require.NoError(t, err)
	assert.Equal(t, "sha256", digest.Algorithm)
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", digest.Hex)
}

func TestNewDigestValidatesHex(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		value     string
	}{
		{
			name:      "non-hex value",
			algorithm: "sha256",
			value:     "not-hex",
		},
		{
			name:      "unsupported algorithm",
			algorithm: "md5",
			value:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name:      "short sha1",
			algorithm: "sha1",
			value:     "aa",
		},
		{
			name:      "short sha256",
			algorithm: "sha256",
			value:     "aa",
		},
		{
			name:      "long sha256",
			algorithm: "sha256",
			value:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDigest(tt.algorithm, tt.value)

			require.Error(t, err)
			assert.True(t, IsKind(err, KindDigest))
		})
	}
}
