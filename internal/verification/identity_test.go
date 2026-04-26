package verification

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRepositoryTrimsAndValidates(t *testing.T) {
	repository, err := NewRepository(" Owner ", " Repo ")

	require.NoError(t, err)
	assert.Equal(t, Repository{Owner: "Owner", Name: "Repo"}, repository)
	assert.Equal(t, "Owner/Repo", repository.String())
	assert.False(t, repository.IsZero())
}

func TestParseRepositoryParsesOwnerRepo(t *testing.T) {
	repository, err := ParseRepository("owner/repo")

	require.NoError(t, err)
	assert.Equal(t, Repository{Owner: "owner", Name: "repo"}, repository)
}

func TestRepositoryValidateRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name       string
		repository Repository
	}{
		{name: "empty owner", repository: Repository{Name: "repo"}},
		{name: "empty name", repository: Repository{Owner: "owner"}},
		{name: "slash owner", repository: Repository{Owner: "org/team", Name: "repo"}},
		{name: "slash name", repository: Repository{Owner: "owner", Name: "repo/extra"}},
		{name: "backslash owner", repository: Repository{Owner: `org\team`, Name: "repo"}},
		{name: "control name", repository: Repository{Owner: "owner", Name: "repo\n"}},
		{name: "untrimmed owner", repository: Repository{Owner: " owner ", Name: "repo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.repository.Validate())
		})
	}
}

func TestParseRepositoryRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "owner", "owner/repo/extra", "owner/", "/repo"} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseRepository(value)

			require.Error(t, err)
		})
	}
}

func TestRepositoryEqualIsCaseInsensitive(t *testing.T) {
	assert.True(t, Repository{Owner: "OWNER", Name: "Repo"}.Equal(Repository{Owner: "owner", Name: "repo"}))
	assert.False(t, Repository{Owner: "owner", Name: "one"}.Equal(Repository{Owner: "owner", Name: "two"}))
}

func TestRepositoryJSONShapeRemainsObject(t *testing.T) {
	//nolint:musttag // Repository JSON intentionally preserves exported field names.
	raw, err := json.Marshal(Repository{Owner: "owner", Name: "repo"})

	require.NoError(t, err)
	assert.JSONEq(t, `{"Owner":"owner","Name":"repo"}`, string(raw))
}

func TestNewReleaseTagAcceptsGitTagRefNames(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		ref  SourceRef
	}{
		{name: "semantic tag", tag: "v1.2.3", ref: "refs/tags/v1.2.3"},
		{name: "slash delimited tag", tag: "release/v1.2.3", ref: "refs/tags/release/v1.2.3"},
		{name: "build metadata", tag: "v1.2.3+build.1", ref: "refs/tags/v1.2.3+build.1"},
		{name: "github encoded characters", tag: "v#1%1", ref: "refs/tags/v#1%1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, err := NewReleaseTag(tt.tag)

			require.NoError(t, err)
			assert.Equal(t, ReleaseTag(tt.tag), tag)
			assert.Equal(t, tt.tag, tag.String())
			assert.Equal(t, tt.ref, tag.RefName())
		})
	}
}

func TestNewReleaseTagRejectsUnsafeGitRefNames(t *testing.T) {
	tests := []struct {
		name string
		tag  string
	}{
		{name: "empty", tag: ""},
		{name: "leading whitespace", tag: " v1.2.3"},
		{name: "control character", tag: "v1.2.3\n"},
		{name: "traversal", tag: "release/../v1.2.3"},
		{name: "double dot", tag: "release..v1.2.3"},
		{name: "reflog syntax", tag: "release@{v1.2.3"},
		{name: "leading slash", tag: "/v1.2.3"},
		{name: "trailing slash", tag: "v1.2.3/"},
		{name: "double slash", tag: "release//v1.2.3"},
		{name: "lock suffix", tag: "release.lock"},
		{name: "dot prefix component", tag: ".release/v1.2.3"},
		{name: "dot suffix component", tag: "release./v1.2.3"},
		{name: "space", tag: "release v1.2.3"},
		{name: "tilde", tag: "release~v1.2.3"},
		{name: "caret", tag: "release^v1.2.3"},
		{name: "colon", tag: "release:v1.2.3"},
		{name: "question", tag: "release?v1.2.3"},
		{name: "asterisk", tag: "release*v1.2.3"},
		{name: "open bracket", tag: "release[v1.2.3"},
		{name: "backslash", tag: `release\v1.2.3`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewReleaseTag(tt.tag)

			require.Error(t, err)
		})
	}
}

func TestNewSourceRefRequiresFullyQualifiedGitRef(t *testing.T) {
	ref, err := NewSourceRef("refs/tags/release/v1.2.3")

	require.NoError(t, err)
	assert.Equal(t, SourceRef("refs/tags/release/v1.2.3"), ref)
	assert.Equal(t, "refs/tags/release/v1.2.3", ref.String())
	assert.False(t, ref.IsZero())

	_, err = NewSourceRef("v1.2.3")
	require.Error(t, err)
}

func TestNewWorkflowIdentityCanonicalizesGitHubWorkflow(t *testing.T) {
	workflow, err := NewWorkflowIdentity("https://github.com/Owner/Repo/.github/workflows/release.yml@refs/heads/main")

	require.NoError(t, err)
	assert.Equal(t, WorkflowIdentity("Owner/Repo/.github/workflows/release.yml@refs/heads/main"), workflow)
	assert.Equal(t, "Owner/Repo/.github/workflows/release.yml@refs/heads/main", workflow.String())
}

func TestNewWorkflowIdentityRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "leading whitespace", value: " owner/repo/.github/workflows/release.yml"},
		{name: "http url", value: "http://github.com/owner/repo/.github/workflows/release.yml"},
		{name: "wrong host", value: "https://example.com/owner/repo/.github/workflows/release.yml"},
		{name: "missing repo", value: "owner/.github/workflows/release.yml"},
		{name: "outside workflow directory", value: "owner/repo/.github/actions/release.yml"},
		{name: "absolute path", value: "/owner/repo/.github/workflows/release.yml"},
		{name: "empty path component", value: "owner/repo/.github/workflows//release.yml"},
		{name: "path traversal", value: "owner/repo/.github/workflows/../release.yml"},
		{name: "backslash", value: `owner/repo/.github/workflows\release.yml`},
		{name: "control character", value: "owner/repo/.github/workflows/release.yml\n"},
		{name: "non yaml", value: "owner/repo/.github/workflows/release.json"},
		{name: "empty ref", value: "owner/repo/.github/workflows/release.yml@"},
		{name: "invalid ref", value: "owner/repo/.github/workflows/release.yml@main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWorkflowIdentity(tt.value)

			require.Error(t, err)
		})
	}
}

func TestWorkflowIdentitySameWorkflowPathIgnoresRefAndURLForm(t *testing.T) {
	assert.True(t,
		WorkflowIdentity("meigma/ghd-test/.github/workflows/release.yml").SameWorkflowPath(
			WorkflowIdentity("https://github.com/meigma/ghd-test/.github/workflows/release.yml@refs/tags/v1.0.1"),
		),
	)
	assert.False(t,
		WorkflowIdentity("meigma/ghd-test/.github/workflows/release.yml").SameWorkflowPath(
			WorkflowIdentity("https://github.com/meigma/ghd-test/.github/workflows/release-v2.yml@refs/tags/v1.0.4"),
		),
	)
}
