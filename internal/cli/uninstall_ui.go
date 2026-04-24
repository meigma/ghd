package cli

import (
	"fmt"
	"io"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/state"
)

type uninstallPresentationMode struct {
	presentationMode
}

func detectUninstallPresentationMode(options Options) uninstallPresentationMode {
	nonInteractive := options.Viper.GetBool("non-interactive")
	errTTY := errorIsTerminal(options)
	enhanced := !nonInteractive && errTTY
	color := enhanced && colorEnabledForOptions(options)
	return uninstallPresentationMode{
		presentationMode: presentationMode{
			nonInteractive: nonInteractive,
			color:          color,
			enhanced:       enhanced,
			statusLine:     enhanced,
		},
	}
}

func (s *statusLine) UpdateUninstallProgress(progress app.UninstallProgress) {
	s.Update(progress.Message)
}

func writeUninstallSummary(w io.Writer, record state.Record, enhanced bool, color bool) {
	target := terminalSafeText(record.Repository + "/" + record.Package + "@" + record.Version)
	if !enhanced {
		fmt.Fprintf(w, "uninstalled %s\n", target)
		return
	}

	styles := newUIStyles(color)
	fmt.Fprintln(w, styles.title.Render(fmt.Sprintf("uninstalled %s", target)))
	fmt.Fprintln(w)
	fmt.Fprintln(w, formatRows([]uiRow{
		{"Asset", record.Asset},
		{"Store", record.StorePath},
		{"Evidence", record.VerificationPath},
	}, 9))
	if len(record.Binaries) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, styles.label.Render("binaries"))
	for _, binary := range record.Binaries {
		fmt.Fprintf(w, "  %s\n", terminalSafeText(binary.LinkPath))
	}
}
