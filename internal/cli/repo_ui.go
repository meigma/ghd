package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/verification"
)

type repoMutationPresentationMode struct {
	enhanced   bool
	color      bool
	statusLine bool
}

func detectRepositoryMutationMode(options Options) repoMutationPresentationMode {
	enhanced := !options.Viper.GetBool("non-interactive") && errorIsTerminal(options)
	color := enhanced && colorEnabledForOptions(options)
	return repoMutationPresentationMode{
		enhanced:   enhanced,
		color:      color,
		statusLine: enhanced,
	}
}

func writeRepositoryAddSummary(w io.Writer, record catalog.RepositoryRecord, enhanced bool, color bool) {
	if !enhanced {
		fmt.Fprintf(w, "indexed %s\n", record.Repository)
		return
	}

	styles := newUIStyles(color)
	var b strings.Builder
	fmt.Fprintln(&b, styles.title.Render(fmt.Sprintf("indexed %s", terminalSafeText(record.Repository.String()))))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, formatRows([]uiRow{
		{"Repository", record.Repository.String()},
		{"Packages", fmt.Sprint(len(record.Packages))},
		{"Commands", strings.Join(repositoryPackageBinaryNames(record), ", ")},
	}, 10))
	fmt.Fprintln(&b)
	fmt.Fprint(&b, styles.muted.Render("The local index was updated."))
	fmt.Fprintln(w, strings.TrimRight(b.String(), "\n"))
}

func writeRepositoryRefreshSummary(w io.Writer, repositories []catalog.RepositoryRecord, target verification.Repository, enhanced bool, color bool) {
	if !enhanced {
		if !target.IsZero() {
			fmt.Fprintf(w, "refreshed %s\n", target)
			return
		}
		fmt.Fprintf(w, "refreshed %d repositories\n", len(repositories))
		return
	}

	styles := newUIStyles(color)
	var b strings.Builder
	if !target.IsZero() {
		record := repositories[0]
		fmt.Fprintln(&b, styles.title.Render(fmt.Sprintf("refreshed %s", terminalSafeText(target.String()))))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, formatRows([]uiRow{
			{"Repository", record.Repository.String()},
			{"Packages", fmt.Sprint(len(record.Packages))},
			{"Commands", strings.Join(repositoryPackageBinaryNames(record), ", ")},
		}, 10))
		fmt.Fprintln(w, strings.TrimRight(b.String(), "\n"))
		return
	}

	packageCount := 0
	for _, record := range repositories {
		packageCount += len(record.Packages)
	}
	fmt.Fprintln(&b, styles.title.Render(fmt.Sprintf("refreshed %d repositories", len(repositories))))
	if len(repositories) != 0 {
		fmt.Fprintln(&b)
		for _, record := range repositories {
			fmt.Fprintln(&b, styles.accent.Render(terminalSafeText(record.Repository.String())))
			fmt.Fprintln(&b, formatRows([]uiRow{
				{"Packages", fmt.Sprint(len(record.Packages))},
				{"Commands", strings.Join(repositoryPackageBinaryNames(record), ", ")},
			}, 8))
			fmt.Fprintln(&b)
		}
	}
	fmt.Fprint(&b, styles.muted.Render(fmt.Sprintf("%d repositories, %d packages", len(repositories), packageCount)))
	fmt.Fprintln(w, strings.TrimRight(b.String(), "\n"))
}

func writeRepositoryRemoveSummary(w io.Writer, repository verification.Repository, enhanced bool, color bool) {
	if !enhanced {
		fmt.Fprintf(w, "removed %s\n", repository)
		return
	}

	styles := newUIStyles(color)
	var b strings.Builder
	fmt.Fprintln(&b, styles.title.Render(fmt.Sprintf("removed %s", terminalSafeText(repository.String()))))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, formatRows([]uiRow{
		{"Repository", repository.String()},
		{"Scope", "Local index only"},
	}, 10))
	fmt.Fprintln(&b)
	fmt.Fprint(&b, styles.muted.Render("Installed packages from this repository, if any, were not changed."))
	fmt.Fprintln(w, strings.TrimRight(b.String(), "\n"))
}

func repositoryPackageBinaryNames(record catalog.RepositoryRecord) []string {
	names := make([]string, 0, len(record.Packages))
	for _, pkg := range record.Packages {
		names = append(names, pkg.Binaries...)
	}
	if len(names) == 0 {
		return []string{"(none)"}
	}
	return terminalSafeStrings(names)
}
