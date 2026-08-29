package cli

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"artifactd/internal/model"
	"github.com/spf13/cobra"
)

func versionsCommand(app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "versions <artifact-id>",
		Short: "List artifact versions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.client()
			if err != nil {
				return err
			}
			versions, err := client.Versions(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeVersions(cmd, versions, app.output)
		},
	}
}

func restoreCommand(app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <artifact-id> <version>",
		Short: "Restore an artifact version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := strconv.Atoi(args[1])
			if err != nil || version < 1 {
				return fmt.Errorf("version must be a positive integer")
			}
			client, err := app.client()
			if err != nil {
				return err
			}
			response, err := client.Restore(cmd.Context(), args[0], version)
			if err != nil {
				return err
			}
			return writeOutput(cmd, app.output, response, func(out io.Writer) error {
				if _, err := fmt.Fprintf(out, "%s restored to version %d\n", response.Artifact.ID, response.Version); err != nil {
					return fmt.Errorf("writing restore result: %w", err)
				}
				return nil
			})
		},
	}
}

func writeVersions(cmd *cobra.Command, versions []model.Version, output string) error {
	return writeOutput(cmd, output, versions, func(out io.Writer) error {
		writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(writer, "VERSION\tENTRY\tWORKSPACE\tCREATED"); err != nil {
			return fmt.Errorf("writing version header: %w", err)
		}
		for _, version := range versions {
			if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%s\n", version.Number, version.Entry, version.WorkspaceID, version.CreatedAt.Format("2006-01-02 15:04:05")); err != nil {
				return fmt.Errorf("writing version: %w", err)
			}
		}
		return writer.Flush()
	})
}
