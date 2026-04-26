package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/verification"
)

func TestUpdateApprovalSummaryHighlightsCriticalDetails(t *testing.T) {
	approval := app.UpdateApproval{
		Repository:              verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:             "foo",
		PreviousVersion:         "1.2.3",
		Version:                 "1.3.0",
		CandidateSignerWorkflow: "owner/repo/.github/workflows/release.yml",
		BinDir:                  "/opt/ghd/bin",
		Binaries:                []string{"foo"},
	}

	assert.Equal(t, "Update foo 1.2.3 -> 1.3.0?", updateApprovalTitle(approval))
	summary := updateApprovalSummary(approval)
	assert.Contains(t, summary, "From:")
	assert.Contains(t, summary, "owner/repo")
	assert.Contains(t, summary, "Version:")
	assert.Contains(t, summary, "1.2.3 -> 1.3.0")
	assert.Contains(t, summary, "To:")
	assert.Contains(t, summary, "/opt/ghd/bin/foo")
	assert.Contains(t, summary, "Verified:")
	assert.Contains(t, summary, "GitHub release + SLSA provenance")
	assert.NotContains(t, summary, "Predicate")
}

func TestUpdateApprovalSummaryExplainsSignerChanges(t *testing.T) {
	approval := app.UpdateApproval{
		Repository:              verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:             "foo",
		PreviousVersion:         "1.2.3",
		Version:                 "1.3.0",
		TrustedSignerWorkflow:   "owner/repo/.github/workflows/release.yml",
		CandidateSignerWorkflow: "owner/repo/.github/workflows/release-v2.yml",
		SignerChanged:           true,
		BinDir:                  "/opt/ghd/bin",
		Binaries:                []string{"foo"},
	}

	assert.Equal(t, "Release signer changed for foo 1.2.3 -> 1.3.0", updateApprovalTitle(approval))
	assert.Equal(t, "Approve signer change and update", updateApprovalActionLabel(approval))
	summary := updateApprovalSummary(approval)
	assert.Contains(t, summary, "different release signer")
	assert.Contains(t, summary, "future updates and verify runs")
	assert.Contains(t, summary, "Trusted signer:")
	assert.Contains(t, summary, "owner/repo/.github/workflows/release.yml")
	assert.Contains(t, summary, "New signer:")
	assert.Contains(t, summary, "owner/repo/.github/workflows/release-v2.yml")
}

func TestUpdateApprovalSummaryDisclosesCustomTrustRoot(t *testing.T) {
	approval := app.UpdateApproval{
		Repository:              verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:             "foo",
		PreviousVersion:         "1.2.3",
		Version:                 "1.3.0",
		CandidateSignerWorkflow: "owner/repo/.github/workflows/release.yml",
		TrustRootPath:           "/tmp/trusted_root.json",
	}

	summary := updateApprovalSummary(approval)

	assert.Contains(t, summary, "custom Sigstore trust root + SLSA provenance")
	assert.Contains(t, summary, "Trust root:")
	assert.Contains(t, summary, "/tmp/trusted_root.json")
}

func TestUpdateApprovalDetailsIncludeProvenanceFacts(t *testing.T) {
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	require.NoError(t, err)
	approval := app.UpdateApproval{
		Repository:              verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:             "foo",
		PreviousVersion:         "1.2.3",
		Version:                 "1.3.0",
		Tag:                     "v1.3.0",
		AssetName:               "foo_1.3.0_darwin_arm64.tar.gz",
		AssetDigest:             digest,
		ReleasePredicateType:    verification.ReleasePredicateV02,
		ProvenancePredicateType: verification.SLSAPredicateV1,
		CandidateSignerWorkflow: "owner/repo/.github/workflows/release.yml",
		BinDir:                  "/opt/ghd/bin",
		Binaries:                []string{"foo"},
	}

	details := updateApprovalDescription(approval)
	assert.Contains(t, details, "Previous:")
	assert.Contains(t, details, "1.2.3")
	assert.Contains(t, details, "Current:")
	assert.Contains(t, details, "1.3.0")
	assert.Contains(t, details, "Tag:")
	assert.Contains(t, details, "v1.3.0")
	assert.Contains(t, details, "Asset:")
	assert.Contains(t, details, "foo_1.3.0_darwin_arm64.tar.gz")
	assert.Contains(t, details, "Digest:")
	assert.Contains(t, details, digest.String())
	assert.Contains(t, details, "Release:")
	assert.Contains(t, details, verification.ReleasePredicateV02)
	assert.Contains(t, details, "Provenance:")
	assert.Contains(t, details, verification.SLSAPredicateV1)
	assert.Contains(t, details, "Signer:")
	assert.Contains(t, details, "owner/repo/.github/workflows/release.yml")
}

func TestUpdateApprovalDetailsShowTrustedAndNewSignerWhenChanged(t *testing.T) {
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	require.NoError(t, err)
	approval := app.UpdateApproval{
		Repository:              verification.Repository{Owner: "owner", Name: "repo"},
		PackageName:             "foo",
		PreviousVersion:         "1.2.3",
		Version:                 "1.3.0",
		Tag:                     "v1.3.0",
		AssetName:               "foo_1.3.0_darwin_arm64.tar.gz",
		AssetDigest:             digest,
		ReleasePredicateType:    verification.ReleasePredicateV02,
		ProvenancePredicateType: verification.SLSAPredicateV1,
		TrustedSignerWorkflow:   "owner/repo/.github/workflows/release.yml",
		CandidateSignerWorkflow: "owner/repo/.github/workflows/release-v2.yml",
		SignerChanged:           true,
		BinDir:                  "/opt/ghd/bin",
		Binaries:                []string{"foo"},
	}

	assert.Equal(t, "Verified signer change details", updateApprovalDetailsTitle(approval))
	details := updateApprovalDescription(approval)
	assert.Contains(t, details, "Trusted signer:")
	assert.Contains(t, details, "owner/repo/.github/workflows/release.yml")
	assert.Contains(t, details, "New signer:")
	assert.Contains(t, details, "owner/repo/.github/workflows/release-v2.yml")
	assert.NotContains(t, details, "Signer:")
}
