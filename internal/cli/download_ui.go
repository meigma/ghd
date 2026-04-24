package cli

import (
	"fmt"
	"io"

	"github.com/meigma/ghd/internal/app"
)

type downloadPresentationMode struct {
	presentationMode
}

func detectDownloadPresentationMode(options Options) downloadPresentationMode {
	nonInteractive := options.Viper.GetBool("non-interactive")
	errTTY := errorIsTerminal(options)
	enhanced := !nonInteractive && errTTY
	color := enhanced && colorEnabledForOptions(options)
	return downloadPresentationMode{
		presentationMode: presentationMode{
			nonInteractive: nonInteractive,
			color:          color,
			enhanced:       enhanced,
			statusLine:     enhanced,
		},
	}
}

func (s *statusLine) UpdateDownloadProgress(progress app.VerifiedDownloadProgress) {
	if progress.Download != nil {
		s.UpdateDownloadAsset(*progress.Download)
		return
	}
	s.Update(progress.Message)
}

func (s *statusLine) UpdateDownloadAsset(progress app.DownloadProgress) {
	line := renderInstallDownloadProgress(progress, s.nextFrame(), s.styles)
	s.UpdateLine(line)
}

func writeDownloadSummary(w io.Writer, result app.VerifiedDownloadResult, enhanced bool, color bool, trustRootPath string) {
	target := terminalSafeText(result.Repository.String() + "/" + result.PackageName + "@" + result.Version)
	if !enhanced {
		fmt.Fprintf(w, "verified %s\n", target)
		return
	}

	styles := newUIStyles(color)
	fmt.Fprintln(w, styles.title.Render(fmt.Sprintf("verified %s", target)))
	fmt.Fprintln(w)
	fmt.Fprintln(w, formatRows([]uiRow{
		{"Tag", string(result.Tag)},
		{"Asset", result.AssetName},
		{"Artifact", result.ArtifactPath},
		{"Evidence", result.EvidencePath},
		{"Digest", result.Evidence.AssetDigest.String()},
		{"Verified", trustRootVerificationLabel(trustRootPath)},
		{"Trust root", trustRootPath},
	}, 10))
}
