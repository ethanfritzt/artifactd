package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

func previewCommand(app *Application) *cobra.Command {
	var open, desktopLaunch bool
	command := &cobra.Command{
		Use:   "preview <markdown-file>",
		Short: "Open a temporary Markdown preview in Artifactd",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runPreview(cmd, app, args[0], open); err != nil {
				if desktopLaunch {
					notifyPreviewError(err)
				}
				return err
			}
			return nil
		},
	}
	command.Flags().BoolVar(&open, "open", false, "open the preview in the default browser")
	command.Flags().BoolVar(&desktopLaunch, "desktop", false, "report errors as desktop notifications")
	if err := command.Flags().MarkHidden("desktop"); err != nil {
		panic(err)
	}
	return command
}

func runPreview(cmd *cobra.Command, app *Application, path string, open bool) error {
	client, err := app.client()
	if err != nil {
		return err
	}
	result, err := client.Preview(cmd.Context(), path)
	if err != nil {
		return err
	}
	if app.output == "json" {
		if err := writeJSON(cmd, result); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(cmd.OutOrStdout(), result.URL); err != nil {
		return fmt.Errorf("writing preview result: %w", err)
	}
	if open {
		if err := openBrowser(cmd.Context(), result.URL); err != nil {
			return fmt.Errorf("opening Markdown preview: %w", err)
		}
	}
	return nil
}

func notifyPreviewError(err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "notify-send", "--", "Artifactd Markdown Preview", err.Error())
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return
	}
}
