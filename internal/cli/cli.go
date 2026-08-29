package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"artifactd/internal/config"
	"artifactd/internal/create"
	"artifactd/internal/ipc"
	"artifactd/internal/model"
	"artifactd/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Application struct {
	configFile string
	output     string
}

func NewCommand() *cobra.Command {
	v := config.NewViper()
	app := &Application{}
	root := &cobra.Command{
		Use:           "artifact",
		Short:         "Create and publish local artifacts",
		Version:       version.Value,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&app.configFile, "config", "", "config file")
	root.PersistentFlags().StringVar(&app.output, "output", "table", "output format: table or json")
	if err := config.BindFlags(v, root.PersistentFlags()); err != nil {
		panic(err)
	}
	root.AddCommand(
		createCommand(v, app),
		listCommand(v, app),
		publishCommand(v, app),
		workspaceCommand(v, app),
		dataCommand(v, app),
		watchCommand(v, app),
		unwatchCommand(v, app),
		liveCommand(v, app),
		versionsCommand(v, app),
		restoreCommand(v, app),
		archiveCommand(v, app),
		unarchiveCommand(v, app),
	)
	return root
}

func createCommand(v *viper.Viper, app *Application) *cobra.Command {
	var id, path, name, description string
	var force, open bool
	command := &cobra.Command{
		Use:   "create [artifact-id]",
		Short: "Create and publish a standalone artifact scaffold",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			effectiveID := id
			if len(args) == 1 {
				if effectiveID != "" {
					return fmt.Errorf("artifact ID provided both as an argument and with --id")
				}
				effectiveID = args[0]
			}
			directory := path
			if directory == "" {
				if effectiveID == "" {
					return fmt.Errorf("artifact ID is required unless --path is provided")
				}
				directory, err = config.ManagedSourcePath(cfg.DataDir, effectiveID)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(directory), 0o750); err != nil {
					return fmt.Errorf("creating managed source directory: %w", err)
				}
			}
			if err := create.Run(create.Options{
				Directory:   directory,
				ID:          effectiveID,
				Name:        name,
				Description: description,
				Force:       force,
			}); err != nil {
				return err
			}
			if result, publishErr := ipc.NewClient(cfg.SocketPath).Publish(cmd.Context(), directory); publishErr == nil {
				if cmd.OutOrStdout() != cmd.ErrOrStderr() {
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "published: %s\n", result.URL); err != nil {
						return fmt.Errorf("writing publish result: %w", err)
					}
				}
				if open {
					if err := openBrowser(cmd.Context(), result.URL); err != nil {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open artifact in browser: %v\n", err)
					}
				}
			} else if !daemonUnavailable(publishErr) {
				return fmt.Errorf("auto-publishing artifact: %w", publishErr)
			} else if cmd.OutOrStdout() != cmd.ErrOrStderr() {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: artifactd is unavailable; scaffold was not published\n")
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), filepath.Clean(directory)); err != nil {
				return fmt.Errorf("writing create result: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&id, "id", "", "stable artifact ID")
	command.Flags().StringVar(&path, "path", "", "explicit local source directory (default: Artifactd-managed source)")
	command.Flags().StringVar(&name, "name", "", "artifact name")
	command.Flags().StringVar(&description, "description", "", "artifact description")
	command.Flags().BoolVar(&force, "force", false, "overwrite generated files")
	command.Flags().BoolVar(&open, "open", false, "open the published artifact in the default browser")
	return command
}

func daemonUnavailable(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connecting to artifactd") &&
		(strings.Contains(message, "dial unix") || strings.Contains(message, "connection refused") || strings.Contains(message, "no such file"))
}

func listCommand(v *viper.Viper, app *Application) *cobra.Command {
	var includeArchived bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List published artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(v, app.configFile)
			if err != nil {
				return err
			}
			artifacts, err := ipc.NewClient(cfg.SocketPath).List(cmd.Context(), includeArchived)
			if err != nil {
				return err
			}
			return writeList(cmd, artifacts, app.output)
		},
	}
	command.Flags().BoolVar(&includeArchived, "include-archived", false, "include archived artifacts")
	return command
}

func publishCommand(v *viper.Viper, app *Application) *cobra.Command {
	var open bool
	command := &cobra.Command{
		Use:   "publish <directory-or-id>",
		Short: "Publish an artifact to artifactd",
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
			result, err := ipc.NewClient(cfg.SocketPath).Publish(cmd.Context(), directory)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), result.URL); err != nil {
				return fmt.Errorf("writing publish result: %w", err)
			}
			if open {
				if err := openBrowser(cmd.Context(), result.URL); err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open artifact in browser: %v\n", err)
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&open, "open", false, "open the published artifact in the default browser")
	return command
}

func writeList(cmd *cobra.Command, artifacts []model.Artifact, output string) error {
	switch output {
	case "json":
		return json.NewEncoder(cmd.OutOrStdout()).Encode(artifacts)
	case "table":
		writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(writer, "ID\tNAME\tVERSION\tSTATUS\tUPDATED"); err != nil {
			return fmt.Errorf("writing list header: %w", err)
		}
		for _, artifact := range artifacts {
			status := "active"
			if artifact.ArchivedAt != nil {
				status = "archived"
			}
			if _, err := fmt.Fprintf(writer, "%s\t%s\t%d\t%s\t%s\n", artifact.ID, artifact.Name, artifact.CurrentVersion, status, artifact.UpdatedAt.Format("2006-01-02 15:04:05")); err != nil {
				return fmt.Errorf("writing list entry: %w", err)
			}
		}
		return writer.Flush()
	default:
		return fmt.Errorf("unsupported output format %q", output)
	}
}
