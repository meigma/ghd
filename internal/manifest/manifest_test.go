package manifest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/verification"
)

func TestDecodeValidOnePackageConfig(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))

	require.NoError(t, err)
	assert.Equal(t, SchemaVersion, cfg.Version)
	assert.Equal(t, "owner/repo/.github/workflows/release.yml", cfg.Provenance.SignerWorkflow)
	require.Len(t, cfg.Packages, 1)
	assert.Equal(t, "foo", cfg.Packages[0].Name.String())
}

func TestNewPackageNameTrimsAndPreservesCase(t *testing.T) {
	name, err := NewPackageName(" Foo-CLI_1.2 ")

	require.NoError(t, err)
	assert.Equal(t, PackageName("Foo-CLI_1.2"), name)
	assert.Equal(t, "Foo-CLI_1.2", name.String())
	assert.False(t, name.IsZero())
}

func TestNewPackageNameRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "slash", value: "foo/bar"},
		{name: "backslash", value: `foo\bar`},
		{name: "control character", value: "foo\nbar"},
		{name: "space", value: "foo bar"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPackageName(tt.value)

			require.Error(t, err)
		})
	}
}

func TestNewPackageVersionTrimsAndPreservesText(t *testing.T) {
	version, err := NewPackageVersion(" V1.2.3+Build ")

	require.NoError(t, err)
	assert.Equal(t, PackageVersion("V1.2.3+Build"), version)
	assert.Equal(t, "V1.2.3+Build", version.String())
	assert.False(t, version.IsZero())
}

func TestNewPackageVersionRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "slash", value: "1/2"},
		{name: "backslash", value: `1\2`},
		{name: "control character", value: "1\n2"},
		{name: "dot", value: "."},
		{name: "dot dot", value: ".."},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPackageVersion(tt.value)

			require.Error(t, err)
		})
	}
}

func TestPackageResolvesDefaultAndExplicitTagPatterns(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)

	pkg, err := cfg.Package("foo")
	require.NoError(t, err)
	version, err := NewPackageVersion("1.2.3")
	require.NoError(t, err)
	tag, err := pkg.ReleaseTag(version)
	require.NoError(t, err)
	assert.Equal(t, "foo-v1.2.3", string(tag))

	pkg.TagPattern = ""
	tag, err = pkg.ReleaseTag(version)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", string(tag))
}

func TestPackageEffectiveTagPatternUsesSchemaDefault(t *testing.T) {
	pkg := Package{Name: "foo"}
	assert.Equal(t, "v${version}", pkg.EffectiveTagPattern())

	pkg.TagPattern = "foo-v${version}"
	assert.Equal(t, "foo-v${version}", pkg.EffectiveTagPattern())
}

func TestPackageSelectsPlatformAsset(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)
	pkg, err := cfg.Package("foo")
	require.NoError(t, err)
	version, err := NewPackageVersion("1.2.3")
	require.NoError(t, err)

	asset, err := pkg.SelectAsset(Platform{OS: "darwin", Arch: "arm64"}, version)

	require.NoError(t, err)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", asset.Name)
	assert.Equal(t, "darwin", asset.OS)
	assert.Equal(t, "arm64", asset.Arch)
}

func TestAssetResolveNameUsesTypedVersion(t *testing.T) {
	version, err := NewPackageVersion("1.2.3")
	require.NoError(t, err)

	name, err := (Asset{OS: "darwin", Arch: "arm64", Pattern: "foo_${version}.tar.gz"}).ResolveName(version)

	require.NoError(t, err)
	assert.Equal(t, "foo_1.2.3.tar.gz", name)
}

func TestDecodeRejectsInvalidSchemaVersion(t *testing.T) {
	_, err := Decode([]byte(`
version = 2

[provenance]
signer_workflow = "owner/repo/.github/workflows/release.yml"
`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported ghd.toml version")
}

func TestDecodeRejectsInvalidSignerWorkflow(t *testing.T) {
	_, err := Decode([]byte(strings.ReplaceAll(validConfig(), "owner/repo/.github/workflows/release.yml", "owner/repo/.github/actions/release.yml")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "provenance.signer_workflow")
}

func TestDecodeRejectsUnsafeBinaryPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "absolute path", path: "/usr/local/bin/foo"},
		{name: "parent segment", path: "../foo"},
		{name: "nested parent segment", path: "bin/../foo"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(validConfigWithBinaryPath(tt.path)))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "binary path")
		})
	}
}

func TestDecodeRejectsVersionPatternsWithoutExactlyOneToken(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "static tag pattern",
			data: strings.Replace(validConfig(), `tag_pattern = "foo-v${version}"`, `tag_pattern = "foo-v1.2.3"`, 1),
			want: "tag pattern",
		},
		{
			name: "duplicate tag pattern token",
			data: strings.Replace(validConfig(), `tag_pattern = "foo-v${version}"`, `tag_pattern = "foo-${version}-${version}"`, 1),
			want: "tag pattern",
		},
		{
			name: "static asset pattern",
			data: strings.Replace(validConfig(), `pattern = "foo_${version}_darwin_arm64.tar.gz"`, `pattern = "foo_1.2.3_darwin_arm64.tar.gz"`, 1),
			want: "asset pattern",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.data))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), "exactly one")
		})
	}
}

func TestDecodeRejectsControlCharactersInStructuralFields(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "signer workflow",
			data: strings.Replace(validConfig(), `signer_workflow = "owner/repo/.github/workflows/release.yml"`, `signer_workflow = "owner/repo/.github/workflows/release.yml\n"`, 1),
			want: "provenance.signer_workflow",
		},
		{
			name: "tag pattern",
			data: strings.Replace(validConfig(), `tag_pattern = "foo-v${version}"`, `tag_pattern = "foo-v${version}\n"`, 1),
			want: "tag pattern",
		},
		{
			name: "asset pattern",
			data: strings.Replace(validConfig(), `pattern = "foo_${version}_darwin_arm64.tar.gz"`, `pattern = "foo_${version}_darwin_arm64.tar.gz\n"`, 1),
			want: "pattern",
		},
		{
			name: "binary path",
			data: strings.Replace(validConfig(), `path = "bin/foo"`, `path = "bin/foo\n"`, 1),
			want: "binary path",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.data))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), "control")
		})
	}
}

func TestPackageSelectAssetFailsForMissingOrAmbiguousPlatform(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)
	pkg, err := cfg.Package("foo")
	require.NoError(t, err)
	version, err := NewPackageVersion("1.2.3")
	require.NoError(t, err)

	_, err = pkg.SelectAsset(Platform{OS: "linux", Arch: "arm64"}, version)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no asset")

	pkg.Assets = append(pkg.Assets, Asset{OS: "darwin", Arch: "arm64", Pattern: "foo2_${version}.tar.gz"})
	_, err = pkg.SelectAsset(Platform{OS: "darwin", Arch: "arm64"}, version)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple assets")
}

func TestPackageVersionForTagMatchesDefaultAndCustomPatterns(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)
	pkg, err := cfg.Package("foo")
	require.NoError(t, err)

	version, matched, err := pkg.VersionForTag(verification.ReleaseTag("foo-v1.2.3"))
	require.NoError(t, err)
	require.True(t, matched)
	assert.Equal(t, "1.2.3", version.String())

	pkg.TagPattern = ""
	version, matched, err = pkg.VersionForTag(verification.ReleaseTag("v1.2.3"))
	require.NoError(t, err)
	require.True(t, matched)
	assert.Equal(t, "1.2.3", version.String())
}

func TestPackageVersionForTagRejectsNonMatchingTagsAndEmptyVersions(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)
	pkg, err := cfg.Package("foo")
	require.NoError(t, err)

	version, matched, err := pkg.VersionForTag(verification.ReleaseTag("bar-v1.2.3"))
	require.NoError(t, err)
	assert.False(t, matched)
	assert.Empty(t, version)

	version, matched, err = pkg.VersionForTag(verification.ReleaseTag("foo-v"))
	require.NoError(t, err)
	assert.False(t, matched)
	assert.Empty(t, version)
}

func TestPackageVersionForTagRejectsInvalidPatterns(t *testing.T) {
	pkg := Package{Name: "foo", TagPattern: "foo-${version}-${version}"}

	_, _, err := pkg.VersionForTag(verification.ReleaseTag("foo-1.2.3-1.2.3"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestPackageReleaseTagRejectsInvalidExpandedTag(t *testing.T) {
	version, err := NewPackageVersion("1.2.3")
	require.NoError(t, err)
	pkg := Package{Name: "foo", TagPattern: ".bad-${version}"}

	_, err = pkg.ReleaseTag(version)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tag")
}

func TestPackageVersionForTagRejectsUnsafeExtractedVersions(t *testing.T) {
	pkg := Package{Name: "foo", TagPattern: "foo-${version}-end"}

	_, _, err := pkg.VersionForTag(verification.ReleaseTag("foo-1/2-end"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "path separators")
}

func validConfig() string {
	return `
version = 1

[provenance]
signer_workflow = "owner/repo/.github/workflows/release.yml"

[[packages]]
name = "foo"
description = "Foo CLI"
tag_pattern = "foo-v${version}"

[[packages.assets]]
os = "darwin"
arch = "arm64"
pattern = "foo_${version}_darwin_arm64.tar.gz"

[[packages.assets]]
os = "linux"
arch = "amd64"
pattern = "foo_${version}_linux_amd64.tar.gz"

[[packages.binaries]]
path = "bin/foo"
`
}

func validConfigWithBinaryPath(path string) string {
	return `
version = 1

[provenance]
signer_workflow = "owner/repo/.github/workflows/release.yml"

[[packages]]
name = "foo"

[[packages.assets]]
os = "darwin"
arch = "arm64"
pattern = "foo_${version}_darwin_arm64.tar.gz"

[[packages.binaries]]
path = "` + path + `"
`
}
