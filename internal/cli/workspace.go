package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"artifactd/internal/config"
	"artifactd/internal/ipc"
	"artifactd/internal/model"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func workspaceCommand(v *viper.Viper, app *Application) *cobra.Command {
	command := &cobra.Command{
		Use:   "workspace",
		Short: "Manage trusted local workspaces",
	}
	command.AddCommand(workspaceAddCommand(v, app), workspaceListCommand(v, app))
	return command
}

func workspaceAddCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "add <id> <directory>",
		Short: "Register a workspace root",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			workspace, err := ipc.NewClient(cfg.SocketPath).AddWorkspace(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return writeWorkspaces(cmd, []model.Workspace{workspace}, app.output)
		},
	}
}

func workspaceListCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			workspaces, err := ipc.NewClient(cfg.SocketPath).ListWorkspaces(cmd.Context())
			if err != nil {
				return err
			}
			return writeWorkspaces(cmd, workspaces, app.output)
		},
	}
}

func writeJSON(cmd *cobra.Command, value any) error {
	return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
}

func writeWorkspaces(cmd *cobra.Command, workspaces []model.Workspace, output string) error {
	if output == "json" {
		return writeJSON(cmd, workspaces)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q", output)
	}
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "ID\tROOT\tUPDATED"); err != nil {
		return fmt.Errorf("writing workspace header: %w", err)
	}
	for _, workspace := range workspaces {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\n", workspace.ID, filepath.Clean(workspace.Root), workspace.UpdatedAt.Format("2006-01-02 15:04:05")); err != nil {
			return fmt.Errorf("writing workspace: %w", err)
		}
	}
	return writer.Flush()
}
