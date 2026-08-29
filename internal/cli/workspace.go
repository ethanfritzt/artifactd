package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"artifactd/internal/model"
	"github.com/spf13/cobra"
)

func workspaceCommand(app *Application) *cobra.Command {
	command := &cobra.Command{
		Use:   "workspace",
		Short: "Manage trusted local workspaces",
	}
	command.AddCommand(workspaceAddCommand(app), workspaceListCommand(app))
	return command
}

func workspaceAddCommand(app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "add <id> <directory>",
		Short: "Register a workspace root",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.client()
			if err != nil {
				return err
			}
			workspace, err := client.AddWorkspace(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return writeWorkspaces(cmd, []model.Workspace{workspace}, app.output)
		},
	}
}

func workspaceListCommand(app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := app.client()
			if err != nil {
				return err
			}
			workspaces, err := client.ListWorkspaces(cmd.Context())
			if err != nil {
				return err
			}
			return writeWorkspaces(cmd, workspaces, app.output)
		},
	}
}

func writeWorkspaces(cmd *cobra.Command, workspaces []model.Workspace, output string) error {
	return writeOutput(cmd, output, workspaces, func(out io.Writer) error {
		writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(writer, "ID\tROOT\tUPDATED"); err != nil {
			return fmt.Errorf("writing workspace header: %w", err)
		}
		for _, workspace := range workspaces {
			if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\n", workspace.ID, filepath.Clean(workspace.Root), workspace.UpdatedAt.Format("2006-01-02 15:04:05")); err != nil {
				return fmt.Errorf("writing workspace: %w", err)
			}
		}
		return writer.Flush()
	})
}
