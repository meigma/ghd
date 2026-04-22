package verification

import "strings"

// Repository identifies a GitHub repository.
type Repository struct {
	// Owner is the GitHub account or organization that owns the repository.
	Owner string
	// Name is the GitHub repository name without the owner.
	Name string
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

func (r Repository) valid() bool {
	return r.Owner != "" && r.Name != "" && !strings.Contains(r.Owner, "/") && !strings.Contains(r.Name, "/")
}

func (r Repository) equal(other Repository) bool {
	return strings.EqualFold(r.Owner, other.Owner) && strings.EqualFold(r.Name, other.Name)
}

// ReleaseTag identifies a GitHub release tag.
type ReleaseTag string

// WorkflowIdentity identifies a trusted GitHub Actions workflow path.
type WorkflowIdentity string

func (w WorkflowIdentity) matches(observed WorkflowIdentity) bool {
	expected := splitWorkflowIdentity(string(w))
	actual := splitWorkflowIdentity(string(observed))
	if expected.path == "" {
		return false
	}
	if !expected.pathMatches(actual.path) {
		return false
	}
	if expected.ref == "" {
		return true
	}
	return expected.ref == actual.ref
}

type workflowIdentity struct {
	path string
	ref  string
}

func (w workflowIdentity) pathMatches(observed string) bool {
	expectedParts := strings.Split(w.path, "/")
	observedParts := strings.Split(observed, "/")
	if len(expectedParts) != len(observedParts) {
		return false
	}
	for i := range expectedParts {
		if i < 2 {
			if !strings.EqualFold(expectedParts[i], observedParts[i]) {
				return false
			}
			continue
		}
		if expectedParts[i] != observedParts[i] {
			return false
		}
	}
	return true
}

func splitWorkflowIdentity(value string) workflowIdentity {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "github.com/")
	if beforeRef, ref, found := strings.Cut(value, "@"); found {
		return workflowIdentity{
			path: strings.Trim(beforeRef, "/"),
			ref:  strings.TrimSpace(ref),
		}
	}
	return workflowIdentity{path: strings.Trim(value, "/")}
}
