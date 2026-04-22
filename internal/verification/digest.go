package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

var supportedDigestLengths = map[string]int{
	"sha1":   40,
	"sha256": 64,
}

// Digest identifies an artifact or release ref digest.
type Digest struct {
	// Algorithm is the lowercase digest algorithm, such as sha1 or sha256.
	Algorithm string
	// Hex is the lowercase hex-encoded digest value.
	Hex string
}

// NewDigest validates and constructs a digest.
func NewDigest(algorithm string, value string) (Digest, error) {
	digest := Digest{
		Algorithm: strings.ToLower(strings.TrimSpace(algorithm)),
		Hex:       strings.ToLower(strings.TrimSpace(value)),
	}
	if digest.Algorithm == "" {
		return Digest{}, newError(KindDigest, "digest algorithm must be set")
	}
	if digest.Hex == "" {
		return Digest{}, newError(KindDigest, "digest value must be set")
	}
	if _, err := hex.DecodeString(digest.Hex); err != nil {
		return Digest{}, wrapError(KindDigest, err, "digest value must be hex encoded")
	}
	wantLen, ok := supportedDigestLengths[digest.Algorithm]
	if !ok {
		return Digest{}, newError(KindDigest, "unsupported digest algorithm %q", digest.Algorithm)
	}
	if len(digest.Hex) != wantLen {
		return Digest{}, newError(KindDigest, "%s digest must be %d hex characters, got %d", digest.Algorithm, wantLen, len(digest.Hex))
	}
	return digest, nil
}

// String returns algorithm:hex.
func (d Digest) String() string {
	if d.Algorithm == "" && d.Hex == "" {
		return ""
	}
	return d.Algorithm + ":" + d.Hex
}

// IsZero reports whether d is unset.
func (d Digest) IsZero() bool {
	return d.Algorithm == "" && d.Hex == ""
}

func (d Digest) validate() error {
	if d.IsZero() {
		return newError(KindDigest, "digest must be set")
	}
	_, err := NewDigest(d.Algorithm, d.Hex)
	return err
}

func (d Digest) equal(other Digest) bool {
	return strings.EqualFold(d.Algorithm, other.Algorithm) && strings.EqualFold(d.Hex, other.Hex)
}

// ArtifactDigester computes a digest for a local artifact path.
type ArtifactDigester interface {
	// DigestFile returns the digest for path.
	DigestFile(path string) (Digest, error)
}

// SHA256FileDigester computes SHA-256 digests for local files.
type SHA256FileDigester struct{}

// DigestFile returns the SHA-256 digest for path.
func (SHA256FileDigester) DigestFile(path string) (Digest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Digest{}, wrapError(KindDigest, err, "open artifact")
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return Digest{}, wrapError(KindDigest, err, "read artifact")
	}

	digest, err := NewDigest("sha256", fmt.Sprintf("%x", hash.Sum(nil)))
	if err != nil {
		return Digest{}, err
	}
	return digest, nil
}
