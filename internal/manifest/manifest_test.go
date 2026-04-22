package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeValidOnePackageConfig(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))

	require.NoError(t, err)
	assert.Equal(t, SchemaVersion, cfg.Version)
	assert.Equal(t, "owner/repo/.github/workflows/release.yml", cfg.Provenance.SignerWorkflow)
	require.Len(t, cfg.Packages, 1)
	assert.Equal(t, "foo", cfg.Packages[0].Name)
}

func TestPackageResolvesDefaultAndExplicitTagPatterns(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)

	pkg, err := cfg.Package("foo")
	require.NoError(t, err)
	tag, err := pkg.ReleaseTag("1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "foo-v1.2.3", string(tag))

	pkg.TagPattern = ""
	tag, err = pkg.ReleaseTag("1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", string(tag))
}

func TestPackageSelectsPlatformAsset(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)
	pkg, err := cfg.Package("foo")
	require.NoError(t, err)

	asset, err := pkg.SelectAsset(Platform{OS: "darwin", Arch: "arm64"}, "1.2.3")

	require.NoError(t, err)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", asset.Name)
	assert.Equal(t, "darwin", asset.OS)
	assert.Equal(t, "arm64", asset.Arch)
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

func TestPackageSelectAssetFailsForMissingOrAmbiguousPlatform(t *testing.T) {
	cfg, err := Decode([]byte(validConfig()))
	require.NoError(t, err)
	pkg, err := cfg.Package("foo")
	require.NoError(t, err)

	_, err = pkg.SelectAsset(Platform{OS: "linux", Arch: "arm64"}, "1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no asset")

	pkg.Assets = append(pkg.Assets, Asset{OS: "darwin", Arch: "arm64", Pattern: "foo2_${version}.tar.gz"})
	_, err = pkg.SelectAsset(Platform{OS: "darwin", Arch: "arm64"}, "1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple assets")
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
