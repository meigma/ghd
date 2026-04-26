package verification

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
)

const (
	repositoryIdentityParts = 2
	workflowIdentityParts   = 5
)

// Repository identifies a GitHub repository.
type Repository struct {
	// Owner is the GitHub account or organization that owns the repository.
	Owner string
	// Name is the GitHub repository name without the owner.
	Name string
}

// NewRepository returns a validated repository identity.
func NewRepository(owner, name string) (Repository, error) {
	repository := Repository{
		Owner: strings.TrimSpace(owner),
		Name:  strings.TrimSpace(name),
	}
	if err := repository.Validate(); err != nil {
		return Repository{}, err
	}
	return repository, nil
}

// ParseRepository parses owner/repo into a validated repository identity.
func ParseRepository(value string) (Repository, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "/")
	if len(parts) != repositoryIdentityParts {
		return Repository{}, errors.New("repository must be owner/repo")
	}
	return NewRepository(parts[0], parts[1])
}

// String returns owner/name.
func (r Repository) String() string {
	if r.Owner == "" && r.Name == "" {
		return ""
	}
	return r.Owner + "/" + r.Name
}

// IsZero reports whether r is unset.
func (r Repository) IsZero() bool {
	return r.Owner == "" && r.Name == ""
}

// Validate checks that r is a complete owner/repo identity.
func (r Repository) Validate() error {
	if strings.TrimSpace(r.Owner) == "" || strings.TrimSpace(r.Name) == "" {
		return errors.New("repository must be owner/repo")
	}
	if r.Owner != strings.TrimSpace(r.Owner) || r.Name != strings.TrimSpace(r.Name) {
		return errors.New("repository must be owner/repo")
	}
	if strings.ContainsAny(r.Owner, `/\`) || strings.ContainsAny(r.Name, `/\`) {
		return errors.New("repository must be owner/repo")
	}
	if hasControlCharacter(r.Owner) || hasControlCharacter(r.Name) {
		return errors.New("repository must not contain control characters")
	}
	return nil
}

// Equal reports whether r and other identify the same repository.
func (r Repository) Equal(other Repository) bool {
	return strings.EqualFold(r.Owner, other.Owner) && strings.EqualFold(r.Name, other.Name)
}

// ReleaseTag identifies a GitHub release tag.
type ReleaseTag string

// NewReleaseTag returns a validated GitHub release tag.
func NewReleaseTag(value string) (ReleaseTag, error) {
	tag := ReleaseTag(value)
	if err := tag.Validate(); err != nil {
		return "", err
	}
	return tag, nil
}

// String returns the raw release tag.
func (t ReleaseTag) String() string {
	return string(t)
}

// RefName returns the fully qualified Git ref for t.
func (t ReleaseTag) RefName() SourceRef {
	return SourceRef("refs/tags/" + string(t))
}

// Validate checks that t is safe to use as a Git tag ref name.
func (t ReleaseTag) Validate() error {
	return validateGitRefName("release tag", string(t), false)
}

// SourceRef identifies a fully qualified Git source ref.
type SourceRef string

// NewSourceRef returns a validated fully qualified Git source ref.
func NewSourceRef(value string) (SourceRef, error) {
	ref := SourceRef(value)
	if err := ref.Validate(); err != nil {
		return "", err
	}
	return ref, nil
}

// String returns the raw source ref.
func (r SourceRef) String() string {
	return string(r)
}

// IsZero reports whether r is unset.
func (r SourceRef) IsZero() bool {
	return r == ""
}

// Validate checks that r is a fully qualified Git ref name.
func (r SourceRef) Validate() error {
	ref := string(r)
	if !strings.HasPrefix(ref, "refs/") {
		return errors.New("source ref must start with refs/")
	}
	return validateGitRefName("source ref", ref, true)
}

// WorkflowIdentity identifies a trusted GitHub Actions workflow path.
type WorkflowIdentity string

// NewWorkflowIdentity returns a validated GitHub Actions workflow identity.
func NewWorkflowIdentity(value string) (WorkflowIdentity, error) {
	identity, err := parseWorkflowIdentity(value)
	if err != nil {
		return "", err
	}
	return WorkflowIdentity(identity.String()), nil
}

// String returns the canonical workflow identity.
func (w WorkflowIdentity) String() string {
	return string(w)
}

// Validate checks that w is a GitHub Actions workflow identity.
func (w WorkflowIdentity) Validate() error {
	_, err := parseWorkflowIdentity(string(w))
	return err
}

// SameWorkflowPath reports whether w and other refer to the same workflow file, ignoring any ref qualifier.
func (w WorkflowIdentity) SameWorkflowPath(other WorkflowIdentity) bool {
	left, err := parseWorkflowIdentity(string(w))
	if err != nil {
		return false
	}
	right, err := parseWorkflowIdentity(string(other))
	if err != nil {
		return false
	}
	return left.pathMatches(right)
}

func (w WorkflowIdentity) matches(observed WorkflowIdentity) bool {
	expected, err := parseWorkflowIdentity(string(w))
	if err != nil {
		return false
	}
	actual, err := parseWorkflowIdentity(string(observed))
	if err != nil {
		return false
	}
	if !expected.pathMatches(actual) {
		return false
	}
	if expected.ref.IsZero() {
		return true
	}
	return expected.ref == actual.ref
}

type workflowIdentity struct {
	repository Repository
	path       string
	ref        SourceRef
}

func (w workflowIdentity) String() string {
	value := w.repository.String() + "/" + w.path
	if !w.ref.IsZero() {
		value += "@" + w.ref.String()
	}
	return value
}

func (w workflowIdentity) pathMatches(observed workflowIdentity) bool {
	return w.repository.Equal(observed.repository) && w.path == observed.path
}

//nolint:gocognit // Workflow identity parsing is kept local to preserve exact validation order.
func parseWorkflowIdentity(value string) (workflowIdentity, error) {
	if value == "" || strings.TrimSpace(value) == "" {
		return workflowIdentity{}, errors.New("workflow identity must be set")
	}
	if value != strings.TrimSpace(value) {
		return workflowIdentity{}, errors.New("workflow identity must not contain leading or trailing whitespace")
	}
	if hasControlCharacter(value) {
		return workflowIdentity{}, errors.New("workflow identity must not contain control characters")
	}
	if after, ok := strings.CutPrefix(value, "https://github.com/"); ok {
		value = after
	} else if strings.Contains(value, "://") {
		return workflowIdentity{}, errors.New("workflow identity must be a GitHub workflow path")
	} else {
		value = strings.TrimPrefix(value, "github.com/")
	}

	beforeRef, refValue, hasRef := strings.Cut(value, "@")
	if beforeRef == "" || strings.HasPrefix(beforeRef, "/") || strings.HasSuffix(beforeRef, "/") {
		return workflowIdentity{}, errors.New("workflow identity must be owner/repo/.github/workflows/file.yml")
	}
	if strings.Contains(beforeRef, `\`) || strings.Contains(beforeRef, "//") {
		return workflowIdentity{}, errors.New("workflow identity path must be a relative GitHub path")
	}

	parts := strings.Split(beforeRef, "/")
	if len(parts) < workflowIdentityParts {
		return workflowIdentity{}, errors.New("workflow identity must be owner/repo/.github/workflows/file.yml")
	}
	repository, err := NewRepository(parts[0], parts[1])
	if err != nil {
		return workflowIdentity{}, err
	}
	pathParts := parts[2:]
	if pathParts[0] != ".github" || pathParts[1] != "workflows" {
		return workflowIdentity{}, errors.New("workflow identity path must start with .github/workflows/")
	}
	for _, part := range pathParts {
		if part == "" || part == "." || part == ".." {
			return workflowIdentity{}, errors.New(
				"workflow identity path must not contain empty or traversal components",
			)
		}
		if strings.Contains(part, `\`) || hasControlCharacter(part) {
			return workflowIdentity{}, errors.New("workflow identity path contains unsupported characters")
		}
	}
	fileName := pathParts[len(pathParts)-1]
	ext := strings.ToLower(path.Ext(fileName))
	if ext != ".yml" && ext != ".yaml" {
		return workflowIdentity{}, errors.New("workflow identity file must be a YAML workflow")
	}

	var ref SourceRef
	if hasRef {
		if strings.TrimSpace(refValue) == "" {
			return workflowIdentity{}, errors.New("workflow identity ref must be set")
		}
		ref, err = NewSourceRef(refValue)
		if err != nil {
			return workflowIdentity{}, fmt.Errorf("workflow identity ref: %w", err)
		}
	}
	return workflowIdentity{
		repository: repository,
		path:       strings.Join(pathParts, "/"),
		ref:        ref,
	}, nil
}

func validateGitRefName(label, value string, requireSlash bool) error {
	if value == "" || strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be set", label)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", label)
	}
	if hasControlCharacter(value) {
		return fmt.Errorf("%s must not contain control characters", label)
	}
	if value == "@" {
		return fmt.Errorf("%s must not be @", label)
	}
	if requireSlash && !strings.Contains(value, "/") {
		return fmt.Errorf("%s must contain a slash", label)
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return fmt.Errorf("%s must not contain empty path components", label)
	}
	if strings.Contains(value, "..") || strings.Contains(value, "@{") {
		return fmt.Errorf("%s contains unsupported Git ref syntax", label)
	}
	if strings.ContainsAny(value, " ~^:?*[\\") {
		return fmt.Errorf("%s contains unsupported Git ref characters", label)
	}
	for component := range strings.SplitSeq(value, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("%s must not contain empty or traversal components", label)
		}
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".") ||
			strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("%s contains unsupported Git ref component %q", label, component)
		}
	}
	return nil
}

func hasControlCharacter(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
