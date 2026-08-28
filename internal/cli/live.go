package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"artifactd/internal/config"
	"artifactd/internal/ipc"
	"artifactd/internal/protocol"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func watchCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "watch <directory-or-id>",
		Short: "Watch an artifact and refresh its live preview",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			directory, err := resolveArtifactDirectory(args[0], cfg.DataDir)
			if err != nil {
				return err
			}
			client := ipc.NewClient(cfg.SocketPath)
			response, err := client.Watch(cmd.Context(), directory)
			if err != nil {
				return err
			}
			if err := writeLive(cmd, response, app.output); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			defer stopWatching(client, response.ArtifactID, cmd.ErrOrStderr())
			return monitorWatch(ctx, cmd, client, response.ArtifactID, response.Status, response.Error)
		},
	}
}

func unwatchCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "unwatch <artifact-id>",
		Short: "Stop an artifact live preview",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			if err := ipc.NewClient(cfg.SocketPath).Unwatch(cmd.Context(), args[0]); err != nil {
				return err
			}
			return nil
		},
	}
}

func liveCommand(v *viper.Viper, app *Application) *cobra.Command {
	return &cobra.Command{
		Use:   "live <artifact-id>",
		Short: "Show an artifact live preview status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			response, err := ipc.NewClient(cfg.SocketPath).Live(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeLiveStatus(cmd, response, app.output)
		},
	}
}

func monitorWatch(ctx context.Context, cmd *cobra.Command, client *ipc.Client, artifactID, status, message string) error {
	lastStatus := status
	lastMessage := message
	if message != "" {
		writeWatchMessage(cmd, status, message)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, err := client.Live(ctx, artifactID)
			if err != nil {
				if lastMessage != err.Error() {
					writeWatchMessage(cmd, "error", err.Error())
					lastMessage = err.Error()
				}
				continue
			}
			if info.Status != lastStatus || info.Error != lastMessage {
				writeWatchMessage(cmd, info.Status, info.Error)
				lastStatus = info.Status
				lastMessage = info.Error
			}
		}
	}
}

func stopWatching(client *ipc.Client, artifactID string, diagnostics io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Unwatch(ctx, artifactID); err != nil {
		_, _ = fmt.Fprintf(diagnostics, "stopping live preview: %v\n", err)
	}
}

func writeWatchMessage(cmd *cobra.Command, status, message string) {
	if message == "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "live preview: %s\n", status)
		return
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "live preview: %s: %s\n", status, message)
}

func writeLiveStatus(cmd *cobra.Command, response protocol.LiveResponse, output string) error {
	if output == "json" {
		return writeJSON(cmd, response)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q", output)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "STATUS\tHASH\tDIRECTORY\n%s\t%s\t%s\n", response.Status, response.Hash, response.Directory)
	if err != nil {
		return fmt.Errorf("writing live status: %w", err)
	}
	if response.Error != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "ERROR\t%s\n", response.Error); err != nil {
			return fmt.Errorf("writing live error: %w", err)
		}
	}
	return nil
}

func writeLive(cmd *cobra.Command, response protocol.LiveResponse, output string) error {
	if output == "json" {
		return writeJSON(cmd, response)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q", output)
	}
	if response.URL != "" {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), response.URL); err != nil {
			return fmt.Errorf("writing live URL: %w", err)
		}
	}
	return nil
}
