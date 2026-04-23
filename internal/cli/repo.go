package cli

import (
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
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repository, err := parseRepositoryTarget(args[0])
			if err != nil {
				return err
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			record, err := runtime.AddRepository(cmd.Context(), app.RepositoryAddRequest{
				Repository: repository,
				IndexDir:   cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(options.Err, "indexed %s\n", record.Repository)
			return nil
		},
	}
}

func newRepositoryListCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List indexed repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			result, err := runtime.ListRepositories(cmd.Context(), app.RepositoryListRequest{IndexDir: cfg.IndexDir})
			if err != nil {
				return err
			}
			writeRepositoryList(options, result.Repositories)
			return nil
		},
	}
}

func newRepositoryRemoveCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "remove owner/repo",
		Short: "Remove a repository from the local index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repository, err := parseRepositoryTarget(args[0])
			if err != nil {
				return err
			}
			cfg := config.Load(options.Viper)
			runtime, err := options.RuntimeFactory(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if err := runtime.RemoveRepository(cmd.Context(), app.RepositoryRemoveRequest{
				Repository: repository,
				IndexDir:   cfg.IndexDir,
			}); err != nil {
				return err
			}
			fmt.Fprintf(options.Err, "removed %s\n", repository)
			return nil
		},
	}
}

func newRepositoryRefreshCommand(options Options) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "refresh [owner/repo | --all]",
		Short: "Refresh indexed repository manifests",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("repo refresh accepts owner/repo or --all, not both")
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
			result, err := runtime.RefreshRepositories(cmd.Context(), app.RepositoryRefreshRequest{
				Repository: repository,
				All:        all || len(args) == 0,
				IndexDir:   cfg.IndexDir,
			})
			if err != nil {
				return err
			}
			if len(args) == 1 {
				fmt.Fprintf(options.Err, "refreshed %s\n", repository)
				return nil
			}
			fmt.Fprintf(options.Err, "refreshed %d repositories\n", len(result.Repositories))
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
			packages = append(packages, pkg.Name)
		}
		fmt.Fprintf(options.Out, "%s %s\n", record.Repository, strings.Join(packages, ","))
	}
}
