package verification

// Policy contains producer expectations enforced against provenance.
type Policy struct {
	// TrustedSignerWorkflow is the workflow expected to sign artifact provenance.
	TrustedSignerWorkflow WorkflowIdentity
	// ExpectedSourceRepository is the repository expected to appear in provenance.
	ExpectedSourceRepository Repository
	// ExpectedSourceRef is the optional source ref expected to appear in provenance.
	ExpectedSourceRef string
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
	if !r.Repository.valid() {
		return newError(KindInvalidRequest, "repository must be owner/repo")
	}
	if r.Tag == "" {
		return newError(KindInvalidRequest, "release tag must be set")
	}
	if r.AssetPath == "" {
		return newError(KindInvalidRequest, "asset path must be set")
	}
	if r.Policy.TrustedSignerWorkflow == "" {
		return newError(KindInvalidRequest, "trusted signer workflow must be set")
	}
	if !r.Policy.ExpectedSourceRepository.valid() {
		return newError(KindInvalidRequest, "expected source repository must be owner/repo")
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
