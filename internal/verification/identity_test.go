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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.repository.Validate())
		})
	}
}

func TestParseRepositoryRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "owner", "owner/repo/extra", "owner/", "/repo"} {
		value := value
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
	raw, err := json.Marshal(Repository{Owner: "owner", Name: "repo"})

	require.NoError(t, err)
	assert.JSONEq(t, `{"Owner":"owner","Name":"repo"}`, string(raw))
}
