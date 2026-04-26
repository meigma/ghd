package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/verification"
)

func TestInstallApprovalSummaryHighlightsCriticalDetails(t *testing.T) {
	approval := app.InstallApproval{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		BinDir:      "/opt/ghd/bin",
		Binaries:    []string{"foo"},
	}

	assert.Equal(t, "Install foo 1.2.3?", installApprovalTitle(approval))
	summary := installApprovalSummary(approval)
	assert.Contains(t, summary, "From:")
	assert.Contains(t, summary, "owner/repo")
	assert.Contains(t, summary, "To:")
	assert.Contains(t, summary, "/opt/ghd/bin/foo")
	assert.Contains(t, summary, "Verified:")
	assert.Contains(t, summary, "GitHub release + SLSA provenance")
	assert.NotContains(t, summary, "Predicate")
}

func TestInstallApprovalSummaryDisclosesCustomTrustRoot(t *testing.T) {
	approval := app.InstallApproval{
		Repository:    verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:   "foo",
		Version:       "1.2.3",
		TrustRootPath: "/tmp/trusted_root.json",
	}

	summary := installApprovalSummary(approval)

	assert.Contains(t, summary, "custom Sigstore trust root + SLSA provenance")
	assert.Contains(t, summary, "Trust root:")
	assert.Contains(t, summary, "/tmp/trusted_root.json")
}

func TestInstallApprovalDetailsIncludeProvenanceFacts(t *testing.T) {
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	require.NoError(t, err)
	approval := app.InstallApproval{
		Repository:              verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:             "foo",
		Version:                 "1.2.3",
		Tag:                     "v1.2.3",
		AssetName:               "foo_1.2.3_darwin_arm64.tar.gz",
		AssetDigest:             digest,
		ReleasePredicateType:    verification.ReleasePredicateV02,
		ProvenancePredicateType: verification.SLSAPredicateV1,
		SignerWorkflow:          "owner/repo/.github/workflows/release.yml",
		BinDir:                  "/opt/ghd/bin",
		Binaries:                []string{"foo"},
	}

	details := installApprovalDescription(approval)
	assert.Contains(t, details, "Tag:")
	assert.Contains(t, details, "v1.2.3")
	assert.Contains(t, details, "Asset:")
	assert.Contains(t, details, "foo_1.2.3_darwin_arm64.tar.gz")
	assert.Contains(t, details, "Digest:")
	assert.Contains(t, details, digest.String())
	assert.Contains(t, details, "Release:")
	assert.Contains(t, details, verification.ReleasePredicateV02)
	assert.Contains(t, details, "Provenance:")
	assert.Contains(t, details, verification.SLSAPredicateV1)
	assert.Contains(t, details, "Signer:")
	assert.Contains(t, details, "owner/repo/.github/workflows/release.yml")
}

func TestEscapeHuhNoteDescriptionPreservesLiteralArtifactNames(t *testing.T) {
	got := escapeNoteDescription("Asset: foo_1.2.3_darwin_arm64.tar.gz")

	assert.Equal(t, `Asset: foo\_1.2.3\_darwin\_arm64.tar.gz`, got)
}

func TestRenderInstallDownloadProgressKnownSize(t *testing.T) {
	line := renderInstallDownloadProgress(app.DownloadProgress{
		AssetName:       "foo.tar.gz",
		BytesDownloaded: 512,
		TotalBytes:      1024,
	}, "-", newUIStyles(false))

	assert.Equal(t, "[############------------] Downloading foo.tar.gz 50% 512 B/1.0 KiB", line)
}

func TestRenderInstallDownloadProgressUnknownSize(t *testing.T) {
	line := renderInstallDownloadProgress(app.DownloadProgress{
		AssetName:       "foo.tar.gz",
		BytesDownloaded: 1536,
	}, "-", newUIStyles(false))

	assert.Equal(t, "- Downloading foo.tar.gz 1.5 KiB", line)
}
