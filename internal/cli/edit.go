package cli

import (
	"fmt"
	"io"

	"artifactd/internal/ipc"
	"artifactd/internal/manifest"
	"artifactd/internal/protocol"
	"github.com/spf13/cobra"
)

func editCommand(app *Application) *cobra.Command {
	command := &cobra.Command{
		Use:   "edit",
		Short: "Manage an artifact editing session",
	}
	command.AddCommand(
		beginEditCommand(app),
		progressEditCommand(app),
		commitEditCommand(app),
		abortEditCommand(app),
	)
	return command
}

func beginEditCommand(app *Application) *cobra.Command {
	var message string
	command := &cobra.Command{
		Use:   "begin <directory-or-id>",
		Short: "Begin an artifact editing session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.loadConfig()
			if err != nil {
				return err
			}
			directory, err := resolveArtifactDirectory(args[0], cfg.DataDir)
			if err != nil {
				return err
			}
			session, err := ipc.NewClient(cfg.SocketPath).BeginEdit(cmd.Context(), directory, message)
			if err != nil {
				return err
			}
			return writeEditSession(cmd, session, app.output)
		},
	}
	command.Flags().StringVar(&message, "message", "", "status message")
	return command
}

func progressEditCommand(app *Application) *cobra.Command {
	var sessionID, message string
	command := &cobra.Command{
		Use:   "progress <artifact-id>",
		Short: "Update an artifact editing session message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.client()
			if err != nil {
				return err
			}
			session, err := client.EditProgress(cmd.Context(), args[0], sessionID, message)
			if err != nil {
				return err
			}
			return writeEditSession(cmd, session, app.output)
		},
	}
	command.Flags().StringVar(&sessionID, "session", "", "edit session ID")
	command.Flags().StringVar(&message, "message", "", "status message")
	_ = command.MarkFlagRequired("session")
	_ = command.MarkFlagRequired("message")
	return command
}

func commitEditCommand(app *Application) *cobra.Command {
	var sessionID string
	command := &cobra.Command{
		Use:   "commit <directory-or-id>",
		Short: "Validate and publish an editing session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.loadConfig()
			if err != nil {
				return err
			}
			directory, err := resolveArtifactDirectory(args[0], cfg.DataDir)
			if err != nil {
				return err
			}
			client := ipc.NewClient(cfg.SocketPath)
			artifactManifest, _, err := manifest.ValidateDirectory(directory)
			if err != nil {
				return err
			}
			artifactID := artifactManifest.ID
			session, err := client.EditStatus(cmd.Context(), artifactID)
			if err != nil {
				return err
			}
			result, err := client.EditCommit(cmd.Context(), artifactID, sessionID, session.BaseVersion, directory)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), result.URL); err != nil {
				return fmt.Errorf("writing edit commit result: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&sessionID, "session", "", "edit session ID")
	_ = command.MarkFlagRequired("session")
	return command
}

func abortEditCommand(app *Application) *cobra.Command {
	var sessionID string
	command := &cobra.Command{
		Use:   "abort <artifact-id>",
		Short: "Abort an artifact editing session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.client()
			if err != nil {
				return err
			}
			return client.EditAbort(cmd.Context(), args[0], sessionID)
		},
	}
	command.Flags().StringVar(&sessionID, "session", "", "edit session ID")
	_ = command.MarkFlagRequired("session")
	return command
}

func writeEditSession(cmd *cobra.Command, session protocol.EditSessionResponse, output string) error {
	return writeOutput(cmd, output, session, func(out io.Writer) error {
		_, err := fmt.Fprintf(out, "SESSION\tARTIFACT\tVERSION\tSTATUS\tDIRECTORY\n%s\t%s\t%d\t%s\t%s\n", session.SessionID, session.ArtifactID, session.BaseVersion, session.Status, session.Directory)
		if err != nil {
			return fmt.Errorf("writing edit session: %w", err)
		}
		return nil
	})
}
