package cli

import (
	"fmt"

	"artifactd/internal/config"
	"artifactd/internal/ipc"
	"artifactd/internal/protocol"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func archiveCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <artifact-id>",
		Short: "Archive a published artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			response, err := archiveArtifact(cmd, v, app, args[0])
			if err != nil {
				return err
			}
			return writeArchiveResult(cmd, response, app.output, "archived")
		},
	}
}

func unarchiveCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "unarchive <artifact-id>",
		Short: "Restore an archived artifact to the active library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			response, err := ipc.NewClient(cfg.SocketPath).Unarchive(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeArchiveResult(cmd, response, app.output, "unarchived")
		},
	}
}

func archiveArtifact(cmd *cobra.Command, v *viper.Viper, app *Application, artifactID string) (protocol.ArtifactResponse, error) {
	cfg, err := config.Load(v, app.configFile)
	if err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return ipc.NewClient(cfg.SocketPath).Archive(cmd.Context(), artifactID)
}

func writeArchiveResult(cmd *cobra.Command, response protocol.ArtifactResponse, output, action string) error {
	if output == "json" {
		return writeJSON(cmd, response)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q", output)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", response.Artifact.ID, action); err != nil {
		return fmt.Errorf("writing archive result: %w", err)
	}
	return nil
}
