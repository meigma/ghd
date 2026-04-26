package manifest

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryManifest(t *testing.T) {
	data, err := os.ReadFile("../../ghd.toml")
	require.NoError(t, err)

	cfg, err := Decode(data)
	require.NoError(t, err)
	assert.Equal(t, "meigma/ghd/.github/workflows/release.yml", cfg.Provenance.SignerWorkflow)

	pkg, err := cfg.Package(PackageName("ghd-example"))
	require.NoError(t, err)
	assert.Equal(t, PackageName("ghd-example"), pkg.Name)
	assert.Equal(t, []Binary{{Path: "ghd-example"}}, pkg.Binaries)

	tag, err := pkg.ReleaseTag(PackageVersion("1.2.3"))
	require.NoError(t, err)
	assert.Equal(t, "example-v1.2.3", tag.String())

	asset, err := pkg.SelectAsset(Platform{OS: "darwin", Arch: "arm64"}, PackageVersion("1.2.3"))
	require.NoError(t, err)
	assert.Equal(t, "ghd-example_1.2.3_darwin_arm64", asset.Name)
}
