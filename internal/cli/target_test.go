package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

func TestParsePackageVersionTargetReturnsTypedIdentity(t *testing.T) {
	target, err := parsePackageVersionTarget("download", "Owner/Repo/Foo_CLI@1.2.3")

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "Owner", Name: "Repo"}, target.repository)
	assert.Equal(t, manifest.PackageName("Foo_CLI"), target.packageName)
	assert.Equal(t, manifest.PackageVersion("1.2.3"), target.version)
	assert.True(t, target.qualified)
}

func TestParsePackageVersionTargetRejectsUnsafePackageNamesAndVersions(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty package", value: "owner/repo/@1.2.3"},
		{name: "slash package", value: "owner/repo/foo/bar@1.2.3"},
		{name: "backslash package", value: `owner/repo/foo\bar@1.2.3`},
		{name: "empty version", value: "owner/repo/foo@"},
		{name: "slash version", value: "owner/repo/foo@1/2"},
		{name: "backslash version", value: `owner/repo/foo@1\2`},
		{name: "control version", value: "owner/repo/foo@1\n2"},
		{name: "dot version", value: "owner/repo/foo@."},
		{name: "dot dot version", value: "owner/repo/foo@.."},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePackageVersionTarget("download", tt.value)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "download target must be owner/repo/package@version")
		})
	}
}

func TestParseInstallTargetAcceptsQualifiedAndVersionlessTargets(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		repository  verification.Repository
		packageName manifest.PackageName
		version     manifest.PackageVersion
		qualified   bool
	}{
		{
			name:        "unqualified versionless",
			value:       "foo",
			packageName: "foo",
		},
		{
			name:        "unqualified explicit version",
			value:       "foo@1.2.3",
			packageName: "foo",
			version:     "1.2.3",
		},
		{
			name:        "qualified versionless",
			value:       "owner/repo/foo",
			repository:  verification.Repository{Owner: "owner", Name: "repo"},
			packageName: "foo",
			qualified:   true,
		},
		{
			name:        "qualified explicit version",
			value:       "owner/repo/foo@1.2.3",
			repository:  verification.Repository{Owner: "owner", Name: "repo"},
			packageName: "foo",
			version:     "1.2.3",
			qualified:   true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			target, err := parseInstallTarget(tt.value)

			require.NoError(t, err)
			assert.Equal(t, tt.repository, target.repository)
			assert.Equal(t, tt.packageName, target.packageName)
			assert.Equal(t, tt.version, target.version)
			assert.Equal(t, tt.qualified, target.qualified)
		})
	}
}

func TestParseInstallTargetRejectsUnsafePackageNamesAndVersions(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty package", value: "@1.2.3"},
		{name: "slash package", value: "foo/bar@1.2.3"},
		{name: "backslash package", value: `foo\bar@1.2.3`},
		{name: "empty version", value: "foo@"},
		{name: "slash version", value: "foo@1/2"},
		{name: "backslash version", value: `foo@1\2`},
		{name: "control version", value: "foo@1\n2"},
		{name: "dot version", value: "foo@."},
		{name: "dot dot version", value: "foo@.."},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseInstallTarget(tt.value)

			require.Error(t, err)
			assert.Contains(t, err.Error(), installTargetError)
		})
	}
}
