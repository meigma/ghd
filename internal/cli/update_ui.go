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
	updateApprovalActionUpdate  = "update"
	updateApprovalActionDetails = "details"
	updateApprovalActionSkip    = "skip"
)

type updatePresentationMode struct {
	presentationMode
	yes       bool
	canPrompt bool
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
	inputTTY := readerIsTerminal(options.In)
	errTTY := writerIsTerminal(options.Err)
	canPrompt := !mode.nonInteractive && inputTTY && errTTY
	if options.UpdateConfirmation != nil && !mode.nonInteractive {
		canPrompt = true
	}
	return updatePresentationMode{
		presentationMode: mode,
		yes:              yes,
		canPrompt:        canPrompt,
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
		if mode.yes {
			return nil
		}
		if !mode.canPrompt {
			return fmt.Errorf("update requires approval after verification; rerun with --yes to approve non-interactively")
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

func promptUpdateApproval(ctx context.Context, options Options, mode updatePresentationMode, approval app.UpdateApproval) error {
	for {
		action := updateApprovalActionUpdate
		selectAction := huh.NewSelect[string]().
			Title(updateApprovalTitle(approval)).
			Description(updateApprovalSummary(approval)).
			Options(
				huh.NewOption("Update", updateApprovalActionUpdate),
				huh.NewOption("View details", updateApprovalActionDetails),
				huh.NewOption("Skip", updateApprovalActionSkip),
			).
			Value(&action)
		if err := runUpdateForm(ctx, options, mode, huh.NewGroup(selectAction)); err != nil {
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
			return app.ErrUpdateNotApproved
		}
	}
}

func showUpdateApprovalDetails(ctx context.Context, options Options, mode updatePresentationMode, approval app.UpdateApproval) error {
	note := huh.NewNote().
		Title("Verified update details").
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
	target := strings.TrimSpace(approval.PackageName)
	version := updateVersionChange(approval)
	if target == "" {
		target = "verified artifact"
	}
	if version == "" {
		return fmt.Sprintf("Update %s?", target)
	}
	return fmt.Sprintf("Update %s %s?", target, version)
}

func updateVersionChange(approval app.UpdateApproval) string {
	previous := strings.TrimSpace(approval.PreviousVersion)
	next := strings.TrimSpace(approval.Version)
	if previous == "" {
		return next
	}
	if next == "" {
		return previous
	}
	return previous + " -> " + next
}

func updateApprovalSummary(approval app.UpdateApproval) string {
	return formatRows([]uiRow{
		{"From", approval.Repository.String()},
		{"Version", updateVersionChange(approval)},
		{"To", updateApprovalDestination(approval)},
		{"Verified", "GitHub release + SLSA provenance"},
	}, 9)
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
	return formatRows([]uiRow{
		{"Repository", approval.Repository.String()},
		{"Package", approval.PackageName},
		{"Previous", approval.PreviousVersion},
		{"Current", approval.Version},
		{"Tag", string(approval.Tag)},
		{"Asset", approval.AssetName},
		{"Digest", approval.AssetDigest.String()},
		{"Release", approval.ReleasePredicateType},
		{"Provenance", approval.ProvenancePredicateType},
		{"Signer", string(approval.SignerWorkflow)},
		{"Bin dir", approval.BinDir},
		{"Binaries", strings.Join(approval.Binaries, ", ")},
	}, 10)
}

func writeUpdateSummary(w io.Writer, results []app.UpdateInstalledResult, enhanced bool, color bool) {
	if !enhanced || len(results) == 0 {
		return
	}
	styles := newUIStyles(color)
	if len(results) == 1 {
		result := results[0]
		target := packageTarget(result.Repository, result.Package)
		switch result.Status {
		case app.UpdateStatusUpdated, app.UpdateStatusUpdatedWithWarning:
			fmt.Fprintln(w, styles.title.Render(fmt.Sprintf("updated %s %s -> %s", target, result.PreviousVersion, result.CurrentVersion)))
		case app.UpdateStatusAlreadyUpToDate:
			fmt.Fprintln(w, styles.title.Render(fmt.Sprintf("%s already up to date", target)))
		default:
			fmt.Fprintln(w, styles.title.Render(fmt.Sprintf("could not update %s", target)))
		}
		if result.Reason != "" {
			fmt.Fprintf(w, "%s %s\n", styles.label.Render("reason"), result.Reason)
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
		{"Updated", fmt.Sprint(updated)},
		{"Current", fmt.Sprint(current)},
		{"Warnings", fmt.Sprint(warned)},
		{"Failed", fmt.Sprint(failed)},
	}, 9))
	fmt.Fprintln(w)
}
