package sigstore

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	intoto "github.com/in-toto/attestation/go/v1"
	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/meigma/ghd/internal/verification"
)

const (
	// PublicGoodIssuerOrg is the Fulcio issuer organization for Sigstore Public Good bundles.
	PublicGoodIssuerOrg = "sigstore.dev"
	// GitHubIssuerOrg is the Fulcio issuer organization for GitHub Sigstore bundles.
	GitHubIssuerOrg = "GitHub, Inc."
)

type signedEntityVerifier interface {
	Verify(sigverify.SignedEntity, sigverify.PolicyBuilder) (*sigverify.VerificationResult, error)
}

// Verifier implements verification.BundleVerifier with sigstore-go.
type Verifier struct {
	github       signedEntityVerifier
	publicGood   signedEntityVerifier
	custom       map[string]signedEntityVerifier
	bundleIssuer func(*sigbundle.Bundle) (string, error)
}

type verifierOptions struct {
	trustedMaterial           root.TrustedMaterial
	trustedRootJSON           []byte
	trustedRootPath           string
	githubTrustedMaterial     root.TrustedMaterial
	publicGoodTrustedMaterial root.TrustedMaterial
	signedTimestampThreshold  int
}

// Option configures a Sigstore verifier adapter.
type Option func(*verifierOptions)

// WithTrustedMaterial sets custom Sigstore trust material whose verifier type is inferred from its issuer.
func WithTrustedMaterial(trustedMaterial root.TrustedMaterial) Option {
	return func(opts *verifierOptions) {
		opts.trustedMaterial = trustedMaterial
	}
}

// WithTrustedRootJSON sets a custom Sigstore trusted_root.json document.
func WithTrustedRootJSON(trustedRootJSON []byte) Option {
	return func(opts *verifierOptions) {
		opts.trustedRootJSON = trustedRootJSON
	}
}

// WithTrustedRootPath sets a path to a custom trusted_root.json document.
func WithTrustedRootPath(trustedRootPath string) Option {
	return func(opts *verifierOptions) {
		opts.trustedRootPath = trustedRootPath
	}
}

// WithGitHubTrustedMaterial sets trust material for GitHub-issued bundles.
func WithGitHubTrustedMaterial(trustedMaterial root.TrustedMaterial) Option {
	return func(opts *verifierOptions) {
		opts.githubTrustedMaterial = trustedMaterial
	}
}

// WithPublicGoodTrustedMaterial sets trust material for Sigstore Public Good bundles.
func WithPublicGoodTrustedMaterial(trustedMaterial root.TrustedMaterial) Option {
	return func(opts *verifierOptions) {
		opts.publicGoodTrustedMaterial = trustedMaterial
	}
}

// WithSignedTimestampThreshold sets the signed timestamp threshold for GitHub bundles.
func WithSignedTimestampThreshold(threshold int) Option {
	return func(opts *verifierOptions) {
		opts.signedTimestampThreshold = threshold
	}
}

// NewVerifier creates a Sigstore bundle verifier.
func NewVerifier(options ...Option) (*Verifier, error) {
	opts := verifierOptions{}
	for _, option := range options {
		option(&opts)
	}

	v := &Verifier{
		custom:       map[string]signedEntityVerifier{},
		bundleIssuer: bundleIssuer,
	}

	if opts.githubTrustedMaterial != nil {
		verifier, err := newGitHubVerifier(opts.githubTrustedMaterial, opts.signedTimestampThreshold)
		if err != nil {
			return nil, err
		}
		v.github = verifier
	}

	if opts.publicGoodTrustedMaterial != nil {
		verifier, err := newPublicGoodVerifier(opts.publicGoodTrustedMaterial)
		if err != nil {
			return nil, err
		}
		v.publicGood = verifier
	}

	if opts.trustedMaterial != nil || len(opts.trustedRootJSON) != 0 || opts.trustedRootPath != "" {
		trustedMaterial, err := trustedMaterial(opts)
		if err != nil {
			return nil, err
		}
		if err := v.registerTrustedMaterial(trustedMaterial, opts.signedTimestampThreshold); err != nil {
			return nil, err
		}
	}

	if v.github == nil && v.publicGood == nil && len(v.custom) == 0 {
		return nil, fmt.Errorf("trusted Sigstore material must be configured")
	}
	return v, nil
}

func newVerifierWithCore(verifier signedEntityVerifier) *Verifier {
	return &Verifier{
		github:       verifier,
		custom:       map[string]signedEntityVerifier{},
		bundleIssuer: func(*sigbundle.Bundle) (string, error) { return GitHubIssuerOrg, nil },
	}
}

// Verify verifies a Sigstore bundle and returns trusted verification evidence.
func (v *Verifier) Verify(ctx context.Context, attestation verification.Attestation, expectedSubject verification.Digest) (verification.VerifiedAttestation, error) {
	if err := ctx.Err(); err != nil {
		return verification.VerifiedAttestation{}, err
	}

	bundle, ok := attestation.Bundle.(*sigbundle.Bundle)
	if !ok {
		return verification.VerifiedAttestation{}, fmt.Errorf("attestation %s does not contain a Sigstore bundle", attestation.ID)
	}

	digestBytes, err := hex.DecodeString(expectedSubject.Hex)
	if err != nil {
		return verification.VerifiedAttestation{}, fmt.Errorf("decode expected subject digest: %w", err)
	}

	// Core policy evaluates certificate identity fields after Sigstore validates
	// the bundle, signature, artifact digest binding, and timestamp evidence.
	policy := sigverify.NewPolicy(
		sigverify.WithArtifactDigest(expectedSubject.Algorithm, digestBytes),
		sigverify.WithoutIdentitiesUnsafe(),
	)

	issuer, err := v.bundleIssuer(bundle)
	if err != nil {
		return verification.VerifiedAttestation{}, err
	}
	verifier, err := v.chooseVerifier(issuer)
	if err != nil {
		return verification.VerifiedAttestation{}, err
	}

	result, err := verifier.Verify(bundle, policy)
	if err != nil {
		return verification.VerifiedAttestation{}, err
	}
	return verifiedAttestation(attestation, result)
}

func trustedMaterial(opts verifierOptions) (root.TrustedMaterial, error) {
	if opts.trustedMaterial != nil {
		return opts.trustedMaterial, nil
	}

	data := opts.trustedRootJSON
	if len(data) == 0 && opts.trustedRootPath != "" {
		file, err := os.ReadFile(opts.trustedRootPath)
		if err != nil {
			return nil, fmt.Errorf("read trusted root: %w", err)
		}
		data = file
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("trusted Sigstore material must be configured")
	}

	trustedRoot, err := root.NewTrustedRootFromJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse trusted root: %w", err)
	}
	return trustedRoot, nil
}

func (v *Verifier) registerTrustedMaterial(trustedMaterial root.TrustedMaterial, signedTimestampThreshold int) error {
	cas := trustedMaterial.FulcioCertificateAuthorities()
	if len(cas) == 0 {
		return fmt.Errorf("trusted material has no Fulcio certificate authorities")
	}

	var registered bool
	for _, authority := range cas {
		fulcio, ok := authority.(*root.FulcioCertificateAuthority)
		if !ok {
			return fmt.Errorf("trusted material certificate authority is not Fulcio-backed")
		}
		cert, err := lowestFulcioCertificate(fulcio)
		if err != nil {
			return err
		}
		if len(cert.Issuer.Organization) == 0 {
			continue
		}
		issuer := cert.Issuer.Organization[0]
		switch issuer {
		case GitHubIssuerOrg:
			verifier, err := newGitHubVerifier(trustedMaterial, signedTimestampThreshold)
			if err != nil {
				return err
			}
			v.github = verifier
		case PublicGoodIssuerOrg:
			verifier, err := newPublicGoodVerifier(trustedMaterial)
			if err != nil {
				return err
			}
			v.publicGood = verifier
		default:
			verifier, err := newCustomVerifier(trustedMaterial)
			if err != nil {
				return err
			}
			v.custom[issuer] = verifier
		}
		registered = true
	}
	if !registered {
		return fmt.Errorf("trusted material has no Fulcio issuer organization")
	}
	return nil
}

func (v *Verifier) chooseVerifier(issuer string) (signedEntityVerifier, error) {
	switch issuer {
	case GitHubIssuerOrg:
		if v.github == nil {
			return nil, fmt.Errorf("GitHub Sigstore verifier is not configured")
		}
		return v.github, nil
	case PublicGoodIssuerOrg:
		if v.publicGood == nil {
			return nil, fmt.Errorf("Sigstore Public Good verifier is not configured")
		}
		return v.publicGood, nil
	default:
		verifier, ok := v.custom[issuer]
		if !ok {
			return nil, fmt.Errorf("no Sigstore verifier configured for issuer %q", issuer)
		}
		return verifier, nil
	}
}

func newGitHubVerifier(trustedMaterial root.TrustedMaterial, signedTimestampThreshold int) (*sigverify.Verifier, error) {
	threshold := signedTimestampThreshold
	if threshold == 0 {
		threshold = 1
	}
	verifier, err := sigverify.NewVerifier(trustedMaterial, sigverify.WithSignedTimestamps(threshold))
	if err != nil {
		return nil, fmt.Errorf("create GitHub Sigstore verifier: %w", err)
	}
	return verifier, nil
}

func newPublicGoodVerifier(trustedMaterial root.TrustedMaterial) (*sigverify.Verifier, error) {
	verifier, err := sigverify.NewVerifier(
		trustedMaterial,
		sigverify.WithSignedCertificateTimestamps(1),
		sigverify.WithTransparencyLog(1),
		sigverify.WithObserverTimestamps(1),
	)
	if err != nil {
		return nil, fmt.Errorf("create Sigstore Public Good verifier: %w", err)
	}
	return verifier, nil
}

func newCustomVerifier(trustedMaterial root.TrustedMaterial) (*sigverify.Verifier, error) {
	options := []sigverify.VerifierOption{sigverify.WithObserverTimestamps(1)}
	if len(trustedMaterial.RekorLogs()) > 0 {
		options = append(options, sigverify.WithTransparencyLog(1))
	}

	verifier, err := sigverify.NewVerifier(trustedMaterial, options...)
	if err != nil {
		return nil, fmt.Errorf("create custom Sigstore verifier: %w", err)
	}
	return verifier, nil
}

func bundleIssuer(bundle *sigbundle.Bundle) (string, error) {
	if !bundle.MinVersion("0.2") {
		return "", fmt.Errorf("unsupported bundle version: %s", bundle.MediaType)
	}
	content, err := bundle.VerificationContent()
	if err != nil {
		return "", fmt.Errorf("get bundle verification content: %w", err)
	}
	cert := content.Certificate()
	if cert == nil {
		return "", fmt.Errorf("bundle has no leaf certificate")
	}
	if len(cert.Issuer.Organization) != 1 {
		return "", fmt.Errorf("expected one leaf certificate issuer organization, got %d", len(cert.Issuer.Organization))
	}
	return cert.Issuer.Organization[0], nil
}

func lowestFulcioCertificate(ca *root.FulcioCertificateAuthority) (*x509.Certificate, error) {
	if len(ca.Intermediates) > 0 {
		return ca.Intermediates[0], nil
	}
	if ca.Root != nil {
		return ca.Root, nil
	}
	return nil, fmt.Errorf("Fulcio certificate authority has no certificates")
}

func verifiedAttestation(attestation verification.Attestation, result *sigverify.VerificationResult) (verification.VerifiedAttestation, error) {
	if result == nil {
		return verification.VerifiedAttestation{}, fmt.Errorf("Sigstore verification returned no result")
	}
	if len(result.VerifiedTimestamps) == 0 {
		return verification.VerifiedAttestation{}, fmt.Errorf("Sigstore verification returned no trusted timestamps")
	}
	if result.Signature == nil || result.Signature.Certificate == nil {
		return verification.VerifiedAttestation{}, fmt.Errorf("Sigstore verification returned no certificate evidence")
	}
	if result.Statement == nil {
		return verification.VerifiedAttestation{}, fmt.Errorf("Sigstore verification returned no in-toto statement")
	}

	statement, err := verificationStatement(result.Statement)
	if err != nil {
		return verification.VerifiedAttestation{}, err
	}
	cert, err := certificateEvidence(*result.Signature.Certificate)
	if err != nil {
		return verification.VerifiedAttestation{}, err
	}

	timestamps := make([]verification.VerifiedTimestamp, 0, len(result.VerifiedTimestamps))
	for _, timestamp := range result.VerifiedTimestamps {
		timestamps = append(timestamps, verification.VerifiedTimestamp{
			Kind: timestamp.Type,
			Time: timestamp.Timestamp,
		})
	}

	return verification.VerifiedAttestation{
		Attestation:        attestation,
		Statement:          statement,
		Certificate:        cert,
		VerifiedTimestamps: timestamps,
	}, nil
}

func verificationStatement(statement *intoto.Statement) (verification.Statement, error) {
	subjects := make([]verification.Subject, 0)
	for _, subject := range statement.GetSubject() {
		for algorithm, value := range subject.GetDigest() {
			digest, err := verification.NewDigest(algorithm, value)
			if err != nil {
				return verification.Statement{}, err
			}
			subjects = append(subjects, verification.Subject{
				Name:   subject.GetName(),
				Digest: digest,
			})
		}
	}

	predicate := statement.GetPredicate()
	releaseTag, err := optionalReleaseTag(stringField(predicate, "tag"))
	if err != nil {
		return verification.Statement{}, err
	}
	return verification.Statement{
		PredicateType: statement.GetPredicateType(),
		Subjects:      subjects,
		Predicate: verification.Predicate{
			ReleaseTag: releaseTag,
			BuildType:  stringField(nestedStruct(predicate, "buildDefinition"), "buildType"),
			BuilderID:  stringField(nestedStruct(nestedStruct(predicate, "runDetails"), "builder"), "id"),
		},
	}, nil
}

func certificateEvidence(cert certificate.Summary) (verification.CertificateEvidence, error) {
	sourceDigest, err := optionalDigest(cert.SourceRepositoryDigest)
	if err != nil {
		return verification.CertificateEvidence{}, err
	}
	signerDigest, err := optionalDigest(cert.BuildSignerDigest)
	if err != nil {
		return verification.CertificateEvidence{}, err
	}
	sourceRepository, err := optionalRepository(cert.SourceRepositoryURI)
	if err != nil {
		return verification.CertificateEvidence{}, err
	}
	sourceRef, err := optionalSourceRef(cert.SourceRepositoryRef)
	if err != nil {
		return verification.CertificateEvidence{}, err
	}
	signerWorkflow, err := optionalWorkflowIdentity(cert.BuildSignerURI)
	if err != nil {
		return verification.CertificateEvidence{}, err
	}

	return verification.CertificateEvidence{
		Issuer:                 cert.Issuer,
		SubjectAlternativeName: cert.SubjectAlternativeName,
		SourceRepository:       sourceRepository,
		SourceRef:              sourceRef,
		SourceDigest:           sourceDigest,
		SignerWorkflow:         signerWorkflow,
		SignerDigest:           signerDigest,
		RunnerEnvironment:      verification.RunnerEnvironment(cert.RunnerEnvironment),
	}, nil
}

func optionalReleaseTag(value string) (verification.ReleaseTag, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	tag, err := verification.NewReleaseTag(value)
	if err != nil {
		return "", fmt.Errorf("release predicate tag %q is invalid: %w", value, err)
	}
	return tag, nil
}

func optionalSourceRef(value string) (verification.SourceRef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	sourceRef, err := verification.NewSourceRef(value)
	if err != nil {
		return "", fmt.Errorf("source repository ref %q is invalid: %w", value, err)
	}
	return sourceRef, nil
}

func optionalWorkflowIdentity(value string) (verification.WorkflowIdentity, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	workflowIdentity, err := verification.NewWorkflowIdentity(value)
	if err != nil {
		return "", fmt.Errorf("build signer URI %q is invalid: %w", value, err)
	}
	return workflowIdentity, nil
}

func optionalDigest(value string) (verification.Digest, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return verification.Digest{}, nil
	}
	if algorithm, digest, found := strings.Cut(value, ":"); found {
		return verification.NewDigest(algorithm, digest)
	}
	switch len(value) {
	case 40:
		return verification.NewDigest("sha1", value)
	case 64:
		return verification.NewDigest("sha256", value)
	default:
		return verification.Digest{}, fmt.Errorf("unsupported digest length %d for %q", len(value), value)
	}
}

func optionalRepository(value string) (verification.Repository, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return verification.Repository{}, nil
	}
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "github.com/")
	value = strings.TrimSuffix(value, ".git")
	value = strings.Trim(value, "/")

	repository, err := verification.ParseRepository(value)
	if err != nil {
		return verification.Repository{}, fmt.Errorf("source repository URI %q is not a GitHub owner/repo URI", value)
	}
	return repository, nil
}

func nestedStruct(parent *structpb.Struct, key string) *structpb.Struct {
	if parent == nil {
		return nil
	}
	value := parent.GetFields()[key]
	if value == nil {
		return nil
	}
	return value.GetStructValue()
}

func stringField(parent *structpb.Struct, key string) string {
	if parent == nil {
		return ""
	}
	value := parent.GetFields()[key]
	if value == nil {
		return ""
	}
	return value.GetStringValue()
}
