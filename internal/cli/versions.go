package cli

import (
	"fmt"
	"strconv"
	"text/tabwriter"

	"artifactd/internal/config"
	"artifactd/internal/ipc"
	"artifactd/internal/model"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func versionsCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "versions <artifact-id>",
		Short: "List artifact versions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			versions, err := ipc.NewClient(cfg.SocketPath).Versions(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeVersions(cmd, versions, app.output)
		},
	}
}

func restoreCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <artifact-id> <version>",
		Short: "Restore an artifact version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := strconv.Atoi(args[1])
			if err != nil || version < 1 {
				return fmt.Errorf("version must be a positive integer")
			}
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			response, err := ipc.NewClient(cfg.SocketPath).Restore(cmd.Context(), args[0], version)
			if err != nil {
				return err
			}
			if app.output == "json" {
				return writeJSON(cmd, response)
			}
			if app.output != "table" {
				return fmt.Errorf("unsupported output format %q", app.output)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s restored to version %d\n", response.Artifact.ID, response.Version)
			if err != nil {
				return fmt.Errorf("writing restore result: %w", err)
			}
			return nil
		},
	}
}

func writeVersions(cmd *cobra.Command, versions []model.Version, output string) error {
	if output == "json" {
		return writeJSON(cmd, versions)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q", output)
	}
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "VERSION\tENTRY\tWORKSPACE\tCREATED"); err != nil {
		return fmt.Errorf("writing version header: %w", err)
	}
	for _, version := range versions {
		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%s\n", version.Number, version.Entry, version.WorkspaceID, version.CreatedAt.Format("2006-01-02 15:04:05")); err != nil {
			return fmt.Errorf("writing version: %w", err)
		}
	}
	return writer.Flush()
}
