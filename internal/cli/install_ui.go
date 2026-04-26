package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"

	"github.com/meigma/ghd/internal/app"
)

const (
	installApprovalActionInstall = "install"
	installApprovalActionDetails = "details"
	installApprovalActionCancel  = "cancel"

	installApprovalSummaryLabelWidth     = 9
	installApprovalDescriptionLabelWidth = 10
	installProgressBarWidth              = 24
	percentMultiplier                    = 100
)

type installPresentationMode struct {
	presentationMode

	yes       bool
	canPrompt bool
}

func detectInstallPresentationMode(options Options) installPresentationMode {
	mode := detectPresentationMode(options)
	yes := options.Viper.GetBool("yes")
	inputTTY := readerIsTerminal(options.In)
	errTTY := writerIsTerminal(options.Err)
	canPrompt := !mode.nonInteractive && inputTTY && errTTY
	if options.InstallConfirmation != nil && !mode.nonInteractive {
		canPrompt = true
	}
	return installPresentationMode{
		presentationMode: mode,
		yes:              yes,
		canPrompt:        canPrompt,
	}
}

func (s *statusLine) UpdateInstallProgress(progress app.InstallProgress) {
	if progress.Download != nil {
		s.UpdateInstallDownload(*progress.Download)
		return
	}
	s.Update(progress.Message)
}

func (s *statusLine) UpdateInstallDownload(progress app.DownloadProgress) {
	line := renderInstallDownloadProgress(progress, s.nextFrame(), s.styles)
	s.UpdateLine(line)
}

func installApprovalCallback(
	options Options,
	mode installPresentationMode,
	status *statusLine,
) app.InstallApprovalFunc {
	return func(ctx context.Context, approval app.InstallApproval) error {
		if status != nil {
			status.Clear()
		}
		if mode.yes {
			return nil
		}
		if !mode.canPrompt {
			return errors.New(
				"install requires approval after verification; rerun with --yes to approve non-interactively",
			)
		}
		confirm := options.InstallConfirmation
		if confirm == nil {
			confirm = func(ctx context.Context, approval app.InstallApproval) error {
				return promptInstallApproval(ctx, options, mode, approval)
			}
		}
		return confirm(ctx, approval)
	}
}

func promptInstallApproval(
	ctx context.Context,
	options Options,
	mode installPresentationMode,
	approval app.InstallApproval,
) error {
	for {
		action := installApprovalActionInstall
		selectAction := huh.NewSelect[string]().
			Title(installApprovalTitle(approval)).
			Description(installApprovalSummary(approval)).
			Options(
				huh.NewOption("Install", installApprovalActionInstall),
				huh.NewOption("View details", installApprovalActionDetails),
				huh.NewOption("Cancel", installApprovalActionCancel),
			).
			Value(&action)
		if err := runInstallForm(ctx, options, mode, huh.NewGroup(selectAction)); err != nil {
			return err
		}
		switch action {
		case installApprovalActionInstall:
			return nil
		case installApprovalActionDetails:
			if err := showInstallApprovalDetails(ctx, options, mode, approval); err != nil {
				return err
			}
		default:
			return app.ErrInstallNotApproved
		}
	}
}

func showInstallApprovalDetails(
	ctx context.Context,
	options Options,
	mode installPresentationMode,
	approval app.InstallApproval,
) error {
	note := huh.NewNote().
		Title("Verified artifact details").
		Description(escapeNoteDescription(installApprovalDescription(approval))).
		Next(true).
		NextLabel("Back")
	return runInstallForm(ctx, options, mode, huh.NewGroup(note))
}

func runInstallForm(ctx context.Context, options Options, mode installPresentationMode, groups ...*huh.Group) error {
	if err := runPromptForm(ctx, options, mode.presentationMode, groups...); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return app.ErrInstallNotApproved
		}
		return fmt.Errorf("prompt install approval: %w", err)
	}
	return nil
}

func installApprovalTitle(approval app.InstallApproval) string {
	target := strings.TrimSpace(approval.PackageName.String())
	if strings.TrimSpace(approval.Version.String()) != "" {
		target = strings.TrimSpace(target + " " + approval.Version.String())
	}
	if target == "" {
		target = "verified artifact"
	}
	return fmt.Sprintf("Install %s?", target)
}

func installApprovalSummary(approval app.InstallApproval) string {
	return formatRows([]uiRow{
		{"From", approval.Repository.String()},
		{"To", installApprovalDestination(approval)},
		{"Verified", trustRootVerificationLabel(approval.TrustRootPath)},
		{"Trust root", approval.TrustRootPath},
	}, installApprovalSummaryLabelWidth)
}

func installApprovalDestination(approval app.InstallApproval) string {
	if strings.TrimSpace(approval.BinDir) == "" {
		return strings.Join(approval.Binaries, ", ")
	}
	switch len(approval.Binaries) {
	case 0:
		return approval.BinDir
	case 1:
		return filepath.Join(approval.BinDir, approval.Binaries[0])
	default:
		return fmt.Sprintf("%s (%s)", approval.BinDir, strings.Join(approval.Binaries, ", "))
	}
}

func installApprovalDescription(approval app.InstallApproval) string {
	return formatRows([]uiRow{
		{"Repository", approval.Repository.String()},
		{"Package", approval.PackageName.String()},
		{"Version", approval.Version.String()},
		{"Tag", string(approval.Tag)},
		{"Asset", approval.AssetName},
		{"Digest", approval.AssetDigest.String()},
		{"Release", approval.ReleasePredicateType},
		{"Provenance", approval.ProvenancePredicateType},
		{"Signer", string(approval.SignerWorkflow)},
		{"Trust root", approval.TrustRootPath},
		{"Bin dir", approval.BinDir},
		{"Binaries", strings.Join(approval.Binaries, ", ")},
	}, installApprovalDescriptionLabelWidth)
}

func renderInstallDownloadProgress(progress app.DownloadProgress, frame string, styles uiStyles) string {
	assetName := strings.TrimSpace(progress.AssetName)
	message := "Downloading"
	if assetName != "" {
		message = "Downloading " + terminalSafeText(assetName)
	}
	downloaded := max(progress.BytesDownloaded, 0)
	if progress.TotalBytes <= 0 {
		return fmt.Sprintf("%s %s %s", frame, message, formatByteCount(downloaded))
	}
	total := max(progress.TotalBytes, 1)
	if downloaded > total {
		downloaded = total
	}
	ratio := float64(downloaded) / float64(total)
	return fmt.Sprintf(
		"%s %s %.0f%% %s/%s",
		renderProgressBar(ratio, installProgressBarWidth, styles),
		message,
		ratio*percentMultiplier,
		formatByteCount(downloaded),
		formatByteCount(total),
	)
}

func writeInstallSummary(w io.Writer, result app.VerifiedInstallResult, enhanced bool, color bool) {
	if !enhanced {
		fmt.Fprintf(
			w,
			"installed %s/%s@%s\n",
			terminalSafeText(result.Repository.String()),
			terminalSafeText(result.PackageName.String()),
			terminalSafeText(result.Version.String()),
		)
		if strings.TrimSpace(result.TrustRootPath) != "" {
			fmt.Fprintf(w, "trust-root %s\n", terminalSafeText(result.TrustRootPath))
		}
		return
	}
	styles := newUIStyles(color)
	fmt.Fprintln(
		w,
		styles.title.Render(
			fmt.Sprintf(
				"installed %s/%s@%s",
				terminalSafeText(result.Repository.String()),
				terminalSafeText(result.PackageName.String()),
				terminalSafeText(result.Version.String()),
			),
		),
	)
	if result.AssetName != "" {
		fmt.Fprintf(w, "%s %s\n", styles.label.Render("asset"), terminalSafeText(result.AssetName))
	}
	if result.Evidence.AssetDigest.String() != "" {
		fmt.Fprintf(w, "%s %s\n", styles.label.Render("digest"), result.Evidence.AssetDigest.String())
	}
	if strings.TrimSpace(result.TrustRootPath) != "" {
		fmt.Fprintf(w, "%s %s\n", styles.label.Render("trust root"), terminalSafeText(result.TrustRootPath))
	}
	if len(result.Binaries) > 0 {
		fmt.Fprintf(w, "%s\n", styles.label.Render("binaries"))
		for _, binary := range result.Binaries {
			fmt.Fprintf(w, "  %s\n", terminalSafeText(binary.LinkPath))
		}
	}
}

func trustRootVerificationLabel(path string) string {
	if strings.TrimSpace(path) == "" {
		return "GitHub release + SLSA provenance"
	}
	return "custom Sigstore trust root + SLSA provenance"
}
