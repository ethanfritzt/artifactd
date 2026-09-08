package cli

import (
	"fmt"
	"os"

	"artifactd/internal/desktop"
	"github.com/spf13/cobra"
)

func integrationCommand(app *Application) *cobra.Command {
	command := &cobra.Command{
		Use:   "integration",
		Short: "Manage desktop file-manager integration",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(
		integrationActionCommand(app, "install"),
		integrationActionCommand(app, "uninstall"),
	)
	return command
}

func integrationActionCommand(app *Application, action string) *cobra.Command {
	short := "Add Artifactd to the Open With menu for Markdown files"
	if action == "uninstall" {
		short = "Remove Artifactd from Markdown Open With menus"
	}
	return &cobra.Command{
		Use:   action,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var path string
			var err error
			if action == "install" {
				executable, executableErr := os.Executable()
				if executableErr != nil {
					return fmt.Errorf("finding artifact executable: %w", executableErr)
				}
				path, err = desktop.Install(executable)
			} else {
				path, err = desktop.Uninstall()
			}
			if err != nil {
				return err
			}
			return writeIntegrationResult(cmd, app.output, action+"ed", path)
		},
	}
}

func writeIntegrationResult(cmd *cobra.Command, output, status, path string) error {
	if output == "json" {
		return writeJSON(cmd, map[string]string{"status": status, "path": path})
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", status, path); err != nil {
		return fmt.Errorf("writing integration result: %w", err)
	}
	return nil
}
