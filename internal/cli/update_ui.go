package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/meigma/ghd/internal/app"
)

const (
	updateApprovalActionUpdate  = "update"
	updateApprovalActionDetails = "details"
	updateApprovalActionSkip    = "skip"

	updateApprovalSummaryLabelWidth      = 9
	updateApprovalSignerChangeLabelWidth = 14
	updateApprovalDescriptionLabelWidth  = 14
	updateSummaryLabelWidth              = 9
)

type updatePresentationMode struct {
	presentationMode

	yes                 bool
	canPrompt           bool
	approveSignerChange bool
}

func detectUpdatePresentationMode(options Options, jsonOutput bool) updatePresentationMode {
	mode := detectPresentationMode(options)
	if jsonOutput {
		mode.dynamic = false
		mode.color = false
		mode.enhanced = false
		mode.statusLine = false
		mode.nonInteractive = true
	}
	yes := options.Viper.GetBool("yes")
	approveSignerChange := options.Viper.GetBool("approve-signer-change")
	inputTTY := readerIsTerminal(options.In)
	errTTY := writerIsTerminal(options.Err)
	canPrompt := !mode.nonInteractive && inputTTY && errTTY
	if options.UpdateConfirmation != nil && !mode.nonInteractive {
		canPrompt = true
	}
	return updatePresentationMode{
		presentationMode:    mode,
		yes:                 yes,
		canPrompt:           canPrompt,
		approveSignerChange: approveSignerChange,
	}
}

func (s *statusLine) UpdateUpdateProgress(progress app.UpdateProgress) {
	if progress.Download != nil {
		s.UpdateUpdateDownload(*progress.Download)
		return
	}
	s.Update(progress.Message)
}

func (s *statusLine) UpdateUpdateDownload(progress app.DownloadProgress) {
	line := renderInstallDownloadProgress(progress, s.nextFrame(), s.styles)
	s.UpdateLine(line)
}

func updateApprovalCallback(options Options, mode updatePresentationMode, status *statusLine) app.UpdateApprovalFunc {
	return func(ctx context.Context, approval app.UpdateApproval) error {
		if status != nil {
			status.Clear()
		}
		if updateApprovalPreapproved(approval, mode) {
			return nil
		}
		if !mode.canPrompt {
			return updateApprovalRequiredError(approval)
		}
		confirm := options.UpdateConfirmation
		if confirm == nil {
			confirm = func(ctx context.Context, approval app.UpdateApproval) error {
				return promptUpdateApproval(ctx, options, mode, approval)
			}
		}
		return confirm(ctx, approval)
	}
}

func updateApprovalPreapproved(approval app.UpdateApproval, mode updatePresentationMode) bool {
	if !mode.yes {
		return false
	}
	return !approval.SignerChanged || mode.approveSignerChange
}

func updateApprovalRequiredError(approval app.UpdateApproval) error {
	if approval.SignerChanged {
		return app.ErrUpdateSignerChangeNotApproved
	}
	return errors.New("update requires approval after verification; rerun with --yes to approve non-interactively")
}

func promptUpdateApproval(
	ctx context.Context,
	options Options,
	mode updatePresentationMode,
	approval app.UpdateApproval,
) error {
	for {
		action := updateApprovalActionUpdate
		selectAction := huh.NewSelect[string]().
			Title(updateApprovalTitle(approval)).
			Description(updateApprovalSummary(approval)).
			Options(
				huh.NewOption(updateApprovalActionLabel(approval), updateApprovalActionUpdate),
				huh.NewOption("View details", updateApprovalActionDetails),
				huh.NewOption("Skip", updateApprovalActionSkip),
			).
			Value(&action)
		if err := runUpdateForm(ctx, options, mode, huh.NewGroup(selectAction)); err != nil {
			if approval.SignerChanged && errors.Is(err, app.ErrUpdateNotApproved) {
				return app.ErrUpdateSignerChangeNotApproved
			}
			return err
		}
		switch action {
		case updateApprovalActionUpdate:
			return nil
		case updateApprovalActionDetails:
			if err := showUpdateApprovalDetails(ctx, options, mode, approval); err != nil {
				return err
			}
		default:
			if approval.SignerChanged {
				return app.ErrUpdateSignerChangeNotApproved
			}
			return app.ErrUpdateNotApproved
		}
	}
}

func showUpdateApprovalDetails(
	ctx context.Context,
	options Options,
	mode updatePresentationMode,
	approval app.UpdateApproval,
) error {
	note := huh.NewNote().
		Title(updateApprovalDetailsTitle(approval)).
		Description(escapeNoteDescription(updateApprovalDescription(approval))).
		Next(true).
		NextLabel("Back")
	return runUpdateForm(ctx, options, mode, huh.NewGroup(note))
}

func runUpdateForm(ctx context.Context, options Options, mode updatePresentationMode, groups ...*huh.Group) error {
	if err := runPromptForm(ctx, options, mode.presentationMode, groups...); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return app.ErrUpdateNotApproved
		}
		return fmt.Errorf("prompt update approval: %w", err)
	}
	return nil
}

func updateApprovalTitle(approval app.UpdateApproval) string {
	target := strings.TrimSpace(approval.PackageName.String())
	version := updateVersionChange(approval)
	if target == "" {
		target = "verified artifact"
	}
	if approval.SignerChanged {
		if version == "" {
			return fmt.Sprintf("Release signer changed for %s", target)
		}
		return fmt.Sprintf("Release signer changed for %s %s", target, version)
	}
	if version == "" {
		return fmt.Sprintf("Update %s?", target)
	}
	return fmt.Sprintf("Update %s %s?", target, version)
}

func updateApprovalActionLabel(approval app.UpdateApproval) string {
	if approval.SignerChanged {
		return "Approve signer change and update"
	}
	return "Update"
}

func updateApprovalDetailsTitle(approval app.UpdateApproval) string {
	if approval.SignerChanged {
		return "Verified signer change details"
	}
	return "Verified update details"
}

func updateVersionChange(approval app.UpdateApproval) string {
	previous := strings.TrimSpace(approval.PreviousVersion.String())
	next := strings.TrimSpace(approval.Version.String())
	if previous == "" {
		return next
	}
	if next == "" {
		return previous
	}
	return previous + " -> " + next
}

func updateApprovalSummary(approval app.UpdateApproval) string {
	rows := []uiRow{
		{"From", approval.Repository.String()},
		{"Version", updateVersionChange(approval)},
		{"To", updateApprovalDestination(approval)},
		{"Verified", trustRootVerificationLabel(approval.TrustRootPath)},
		{"Trust root", approval.TrustRootPath},
	}
	if !approval.SignerChanged {
		return formatRows(rows, updateApprovalSummaryLabelWidth)
	}
	rows = []uiRow{
		{"From", approval.Repository.String()},
		{"Version", updateVersionChange(approval)},
		{"Trusted signer", string(approval.TrustedSignerWorkflow)},
		{"New signer", string(approval.CandidateSignerWorkflow)},
		{"To", updateApprovalDestination(approval)},
		{"Verified", trustRootVerificationLabel(approval.TrustRootPath)},
		{"Trust root", approval.TrustRootPath},
	}
	return strings.Join([]string{
		"This update was signed by a different release signer than the one trusted by your current install.",
		"Approving this update will also change the signer trusted for future updates and verify runs for this package.",
		"",
		formatRows(rows, updateApprovalSignerChangeLabelWidth),
	}, "\n")
}

func updateApprovalDestination(approval app.UpdateApproval) string {
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

func updateApprovalDescription(approval app.UpdateApproval) string {
	rows := []uiRow{
		{"Repository", approval.Repository.String()},
		{"Package", approval.PackageName.String()},
		{"Previous", approval.PreviousVersion.String()},
		{"Current", approval.Version.String()},
		{"Tag", string(approval.Tag)},
		{"Asset", approval.AssetName},
		{"Digest", approval.AssetDigest.String()},
		{"Release", approval.ReleasePredicateType},
		{"Provenance", approval.ProvenancePredicateType},
		{"Trust root", approval.TrustRootPath},
		{"Bin dir", approval.BinDir},
		{"Binaries", strings.Join(approval.Binaries, ", ")},
	}
	if approval.SignerChanged {
		rows = append(rows[:9], append([]uiRow{
			{"Trusted signer", string(approval.TrustedSignerWorkflow)},
			{"New signer", string(approval.CandidateSignerWorkflow)},
		}, rows[9:]...)...)
	} else {
		rows = append(rows[:9], append([]uiRow{
			{"Signer", string(approval.CandidateSignerWorkflow)},
		}, rows[9:]...)...)
	}
	return formatRows(rows, updateApprovalDescriptionLabelWidth)
}

func writeUpdateSummary(
	w io.Writer,
	results []app.UpdateInstalledResult,
	enhanced bool,
	color bool,
	trustRootPath string,
) {
	if !enhanced && strings.TrimSpace(trustRootPath) != "" {
		fmt.Fprintf(w, "trust-root %s\n", terminalSafeText(trustRootPath))
		return
	}
	if !enhanced || len(results) == 0 {
		return
	}
	styles := newUIStyles(color)
	if len(results) == 1 {
		result := results[0]
		target := terminalSafeText(packageTarget(result.Repository, result.Package))
		switch result.Status {
		case app.UpdateStatusUpdated, app.UpdateStatusUpdatedWithWarning:
			fmt.Fprintln(
				w,
				styles.title.Render(
					fmt.Sprintf(
						"updated %s %s -> %s",
						target,
						terminalSafeText(result.PreviousVersion),
						terminalSafeText(result.CurrentVersion),
					),
				),
			)
		case app.UpdateStatusAlreadyUpToDate:
			fmt.Fprintln(w, styles.title.Render(fmt.Sprintf("%s already up to date", target)))
		case app.UpdateStatusCannotUpdate:
			fmt.Fprintln(w, styles.title.Render(fmt.Sprintf("could not update %s", target)))
		}
		if result.Reason != "" {
			fmt.Fprintf(w, "%s %s\n", styles.label.Render("reason"), terminalSafeText(result.Reason))
		}
		if strings.TrimSpace(trustRootPath) != "" {
			fmt.Fprintf(w, "%s %s\n", styles.label.Render("trust root"), terminalSafeText(trustRootPath))
		}
		return
	}

	updated := 0
	current := 0
	warned := 0
	failed := 0
	for _, result := range results {
		switch result.Status {
		case app.UpdateStatusUpdated:
			updated++
		case app.UpdateStatusAlreadyUpToDate:
			current++
		case app.UpdateStatusUpdatedWithWarning:
			warned++
		case app.UpdateStatusCannotUpdate:
			failed++
		}
	}
	fmt.Fprintln(w, styles.title.Render("update results"))
	fmt.Fprint(w, formatRows([]uiRow{
		{"Updated", strconv.Itoa(updated)},
		{"Current", strconv.Itoa(current)},
		{"Warnings", strconv.Itoa(warned)},
		{"Failed", strconv.Itoa(failed)},
		{"Trust root", trustRootPath},
	}, updateSummaryLabelWidth))
	fmt.Fprintln(w)
}
