package cli

import (
	"fmt"
	"io"

	"artifactd/internal/protocol"
	"github.com/spf13/cobra"
)

func archiveCommand(app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <artifact-id>",
		Short: "Archive a published artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			response, err := archiveArtifact(cmd, app, args[0])
			if err != nil {
				return err
			}
			return writeArchiveResult(cmd, response, app.output, "archived")
		},
	}
}

func unarchiveCommand(app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "unarchive <artifact-id>",
		Short: "Restore an archived artifact to the active library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.client()
			if err != nil {
				return err
			}
			response, err := client.Unarchive(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeArchiveResult(cmd, response, app.output, "unarchived")
		},
	}
}

func archiveArtifact(cmd *cobra.Command, app *Application, artifactID string) (protocol.ArtifactResponse, error) {
	client, err := app.client()
	if err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return client.Archive(cmd.Context(), artifactID)
}

func writeArchiveResult(cmd *cobra.Command, response protocol.ArtifactResponse, output, action string) error {
	return writeOutput(cmd, output, response, func(out io.Writer) error {
		if _, err := fmt.Fprintf(out, "%s %s\n", response.Artifact.ID, action); err != nil {
			return fmt.Errorf("writing archive result: %w", err)
		}
		return nil
	})
}
