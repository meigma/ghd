package verification

// Policy contains producer expectations enforced against provenance.
type Policy struct {
	// TrustedSignerWorkflow is the workflow expected to sign artifact provenance.
	TrustedSignerWorkflow WorkflowIdentity
	// ExpectedSourceRepository is the repository expected to appear in provenance.
	ExpectedSourceRepository Repository
	// ExpectedSourceRef is the optional source ref expected to appear in provenance.
	ExpectedSourceRef SourceRef
	// ExpectedSourceDigest is the optional source digest expected to appear in provenance.
	ExpectedSourceDigest Digest
	// ExpectedSignerDigest is the optional signer workflow digest expected to appear in provenance.
	ExpectedSignerDigest Digest
}

// Request describes one release asset verification operation.
type Request struct {
	// Repository is the GitHub repository that owns the release and artifact.
	Repository Repository
	// Tag is the release tag being installed.
	Tag ReleaseTag
	// AssetPath is the local downloaded artifact path.
	AssetPath string
	// Policy is the provenance policy to enforce.
	Policy Policy
}

func (r Request) withDefaults() Request {
	if r.Policy.ExpectedSourceRepository.IsZero() {
		r.Policy.ExpectedSourceRepository = r.Repository
	}
	return r
}

func (r Request) validate() error {
	if err := r.Repository.Validate(); err != nil {
		return newError(KindInvalidRequest, "repository must be owner/repo")
	}
	if err := r.Tag.Validate(); err != nil {
		return newError(KindInvalidRequest, "release tag is invalid: %v", err)
	}
	if r.AssetPath == "" {
		return newError(KindInvalidRequest, "asset path must be set")
	}
	if err := r.Policy.TrustedSignerWorkflow.Validate(); err != nil {
		return newError(KindInvalidRequest, "trusted signer workflow is invalid: %v", err)
	}
	if err := r.Policy.ExpectedSourceRepository.Validate(); err != nil {
		return newError(KindInvalidRequest, "expected source repository must be owner/repo")
	}
	if !r.Policy.ExpectedSourceRef.IsZero() {
		if err := r.Policy.ExpectedSourceRef.Validate(); err != nil {
			return newError(KindInvalidRequest, "expected source ref is invalid: %v", err)
		}
	}
	if !r.Policy.ExpectedSourceDigest.IsZero() {
		if err := r.Policy.ExpectedSourceDigest.validate(); err != nil {
			return err
		}
	}
	if !r.Policy.ExpectedSignerDigest.IsZero() {
		if err := r.Policy.ExpectedSignerDigest.validate(); err != nil {
			return err
		}
	}
	return nil
}
