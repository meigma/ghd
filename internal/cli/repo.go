package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/verification"
)

func newRepositoryCommand(options Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage indexed GitHub repositories",
		Example: strings.TrimSpace(`
ghd repo add owner/repo --index-dir ./index
ghd repo list --index-dir ./index
ghd repo refresh --index-dir ./index --all
ghd repo remove owner/repo --index-dir ./index
`),
	}
	cmd.AddCommand(newRepositoryAddCommand(options))
	cmd.AddCommand(newRepositoryListCommand(options))
	cmd.AddCommand(newRepositoryRemoveCommand(options))
	cmd.AddCommand(newRepositoryRefreshCommand(options))
	return cmd
}

func newRepositoryAddCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "add owner/repo",
		Short: "Add a repository to the local index",
		Example: strings.TrimSpace(`
ghd repo add owner/repo --index-dir ./index
ghd --non-interactive repo add owner/repo --index-dir ./index
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectRepositoryMutationMode(options)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}
			repository, err := parseRepositoryTarget(args[0])
			if err != nil {
				return err
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if status != nil {
				status.Show(fmt.Sprintf("Indexing %s", terminalSafeText(repository.String())))
			}
			record, err := runtime.AddRepository(cmd.Context(), app.RepositoryAddRequest{
				Repository: repository,
				IndexDir:   cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			if status != nil {
				status.Clear()
			}
			writeRepositoryAddSummary(options.Err, record, mode.enhanced, mode.color)
			return nil
		},
	}
}

func newRepositoryListCommand(options Options) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List indexed repositories",
		Example: strings.TrimSpace(`
ghd repo list --index-dir ./index
ghd repo list --index-dir ./index --json
ghd --non-interactive repo list --index-dir ./index
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := detectReadOnlyPresentationMode(options, jsonOutput)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if status != nil {
				status.Show("Loading indexed repositories")
			}
			repositories, err := runtime.ListRepositories(cmd.Context(), cfg.IndexDir)
			if err != nil {
				return err
			}
			if status != nil {
				status.Clear()
			}
			if jsonOutput {
				return writeRepositoryListJSON(options, repositories)
			}
			if mode.richOutput {
				writeRepositoryListTTY(options.Out, repositories, mode.color)
				return nil
			}
			writeRepositoryList(options, repositories)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write indexed repositories as JSON")
	return cmd
}

func newRepositoryRemoveCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "remove owner/repo",
		Short: "Remove a repository from the local index",
		Example: strings.TrimSpace(`
ghd repo remove owner/repo --index-dir ./index
ghd --non-interactive repo remove owner/repo --index-dir ./index
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectRepositoryMutationMode(options)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}
			repository, err := parseRepositoryTarget(args[0])
			if err != nil {
				return err
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if status != nil {
				status.Show(fmt.Sprintf("Removing %s from the local index", terminalSafeText(repository.String())))
			}
			if err := runtime.RemoveRepository(cmd.Context(), app.RepositoryRemoveRequest{
				Repository: repository,
				IndexDir:   cfg.IndexDir,
			}); err != nil {
				return err
			}
			if status != nil {
				status.Clear()
			}
			writeRepositoryRemoveSummary(options.Err, repository, mode.enhanced, mode.color)
			return nil
		},
	}
}

//nolint:gocognit // Cobra command construction is mostly declarative CLI wiring.
func newRepositoryRefreshCommand(options Options) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "refresh [owner/repo | --all]",
		Short: "Refresh indexed repository manifests",
		Example: strings.TrimSpace(`
ghd repo refresh owner/repo --index-dir ./index
ghd repo refresh --index-dir ./index --all
ghd --non-interactive repo refresh --index-dir ./index --all
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := detectRepositoryMutationMode(options)
			var status *transientStatusLine
			if mode.statusLine {
				status = newTransientStatusLine(options.Err, mode.color)
				defer status.Clear()
			}
			if all && len(args) > 0 {
				return errors.New("repo refresh accepts owner/repo or --all, not both")
			}
			var repository verification.Repository
			if len(args) == 1 {
				var err error
				repository, err = parseRepositoryTarget(args[0])
				if err != nil {
					return err
				}
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if status != nil {
				if repository.IsZero() {
					status.Show("Refreshing indexed repositories")
				} else {
					status.Show(fmt.Sprintf("Refreshing %s", terminalSafeText(repository.String())))
				}
			}
			repositories, err := runtime.RefreshRepositories(cmd.Context(), app.RepositoryRefreshRequest{
				Repository: repository,
				All:        all || len(args) == 0,
				IndexDir:   cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			if status != nil {
				status.Clear()
			}
			writeRepositoryRefreshSummary(options.Err, repositories, repository, mode.enhanced, mode.color)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "refresh all indexed repositories")
	return cmd
}

func writeRepositoryList(options Options, repositories []catalog.RepositoryRecord) {
	for _, record := range repositories {
		packages := make([]string, 0, len(record.Packages))
		for _, pkg := range record.Packages {
			packages = append(packages, terminalSafeText(pkg.Name))
		}
		fmt.Fprintf(options.Out, "%s %s\n", terminalSafeText(record.Repository.String()), strings.Join(packages, ","))
	}
}
