package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/verification"
)

type readOnlyPresentationMode struct {
	nonInteractive bool
	color          bool
	richOutput     bool
	statusLine     bool
}

func detectReadOnlyPresentationMode(options Options, jsonOutput bool) readOnlyPresentationMode {
	nonInteractive := jsonOutput || options.Viper.GetBool("non-interactive")
	stdoutTTY := outputIsTerminal(options)
	stderrTTY := errorIsTerminal(options)
	richOutput := !nonInteractive && stdoutTTY
	color := richOutput && colorEnabledForOptions(options)
	return readOnlyPresentationMode{
		nonInteractive: nonInteractive,
		color:          color,
		richOutput:     richOutput,
		statusLine:     richOutput && stderrTTY,
	}
}

func writePackageListTTY(w io.Writer, results []app.PackageListResult, repository verification.Repository, color bool) {
	fmt.Fprint(w, renderPackageListTTY(results, repository, color))
}

func renderPackageListTTY(results []app.PackageListResult, repository verification.Repository, color bool) string {
	styles := newUIStyles(color)
	var b strings.Builder

	title := "indexed packages"
	if !repository.IsZero() {
		title = fmt.Sprintf("packages in %s", terminalSafeText(repository.String()))
	}
	fmt.Fprintln(&b, styles.title.Render(title))
	if len(results) == 0 {
		fmt.Fprint(&b, styles.muted.Render("No packages found."))
		return b.String()
	}

	type packageGroup struct {
		repository string
		packages   []app.PackageListResult
	}

	var groups []packageGroup
	for _, result := range results {
		repository := result.Repository.String()
		if len(groups) == 0 || groups[len(groups)-1].repository != repository {
			groups = append(groups, packageGroup{repository: repository})
		}
		groups[len(groups)-1].packages = append(groups[len(groups)-1].packages, result)
	}

	packageCount := 0
	for _, group := range groups {
		packageCount += len(group.packages)
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, styles.accent.Render(terminalSafeText(group.repository)))
		nameWidth := len("package")
		for _, pkg := range group.packages {
			if len(pkg.PackageName) > nameWidth {
				nameWidth = len(pkg.PackageName)
			}
		}
		fmt.Fprintf(&b, "  %-*s %s\n", nameWidth, styles.label.Render("package"), styles.label.Render("binaries"))
		for _, pkg := range group.packages {
			fmt.Fprintf(
				&b,
				"  %-*s %s\n",
				nameWidth,
				terminalSafeText(pkg.PackageName),
				strings.Join(terminalSafeStrings(pkg.Binaries), ", "),
			)
		}
	}

	fmt.Fprintln(&b)
	fmt.Fprint(&b, styles.muted.Render(fmt.Sprintf("%d repositories, %d packages", len(groups), packageCount)))
	return b.String()
}

func writePackageInfoTTY(w io.Writer, result app.PackageInfoResult, color bool) {
	fmt.Fprint(w, renderPackageInfoTTY(result, color))
}

func renderPackageInfoTTY(result app.PackageInfoResult, color bool) string {
	styles := newUIStyles(color)
	var b strings.Builder

	fmt.Fprintln(&b, styles.title.Render(fmt.Sprintf("package %s", terminalSafeText(packageTarget(result.Repository.String(), result.PackageName)))))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, formatRows([]uiRow{
		{"Repository", result.Repository.String()},
		{"Package", result.PackageName},
		{"Signer", string(result.SignerWorkflow)},
		{"Tag pattern", result.TagPattern},
		{"Binaries", strings.Join(result.Binaries, ", ")},
	}, 11))

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, styles.accent.Render("assets"))
	if len(result.Assets) == 0 {
		fmt.Fprint(&b, styles.muted.Render("  No assets declared."))
		return b.String()
	}

	targetWidth := len("target")
	for _, asset := range result.Assets {
		target := terminalSafeText(asset.OS + "/" + asset.Arch)
		if len(target) > targetWidth {
			targetWidth = len(target)
		}
	}

	fmt.Fprintf(&b, "  %-*s %s\n", targetWidth, styles.label.Render("target"), styles.label.Render("pattern"))
	for _, asset := range result.Assets {
		target := terminalSafeText(asset.OS + "/" + asset.Arch)
		fmt.Fprintf(&b, "  %-*s %s\n", targetWidth, target, terminalSafeText(asset.Pattern))
	}
	return b.String()
}

func writeCheckResultsTTY(w io.Writer, results []app.CheckResult, color bool) {
	fmt.Fprint(w, renderCheckResultsTTY(results, color))
}

func renderCheckResultsTTY(results []app.CheckResult, color bool) string {
	styles := newUIStyles(color)
	var b strings.Builder

	fmt.Fprintln(&b, styles.title.Render("update check"))
	if len(results) == 0 {
		fmt.Fprint(&b, styles.muted.Render("No installed packages matched."))
		return b.String()
	}

	type checkSection struct {
		title   string
		results []app.CheckResult
	}

	sections := []checkSection{
		{title: "updates available"},
		{title: "current"},
		{title: "could not determine"},
	}

	for _, result := range results {
		switch result.Status {
		case app.CheckStatusUpdateAvailable:
			sections[0].results = append(sections[0].results, result)
		case app.CheckStatusUpToDate:
			sections[1].results = append(sections[1].results, result)
		case app.CheckStatusCannotDetermine:
			sections[2].results = append(sections[2].results, result)
		default:
			sections[2].results = append(sections[2].results, result)
		}
	}

	for _, section := range sections {
		if len(section.results) == 0 {
			continue
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, styles.accent.Render(section.title))
		for _, result := range section.results {
			target := terminalSafeText(packageTarget(result.Repository, result.Package))
			switch result.Status {
			case app.CheckStatusUpdateAvailable:
				fmt.Fprintf(&b, "  %s  %s -> %s\n", target, terminalSafeText(result.Version), terminalSafeText(result.LatestVersion))
			case app.CheckStatusCannotDetermine:
				fmt.Fprintf(&b, "  %s  %s  %s\n", target, terminalSafeText(result.Version), terminalSafeText(result.Reason))
			default:
				fmt.Fprintf(&b, "  %s  %s\n", target, terminalSafeText(result.Version))
			}
		}
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, styles.accent.Render("summary"))
	fmt.Fprint(&b, formatRows([]uiRow{
		{"Updates", fmt.Sprint(len(sections[0].results))},
		{"Current", fmt.Sprint(len(sections[1].results))},
		{"Failed", fmt.Sprint(len(sections[2].results))},
	}, 7))
	return b.String()
}
