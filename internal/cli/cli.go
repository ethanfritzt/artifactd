package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	v          *viper.Viper
	configFile string
	output     string
}

func (a *Application) loadConfig() (config.Config, error) {
	return config.Load(a.v, a.configFile)
}

func (a *Application) client() (*ipc.Client, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	return ipc.NewClient(cfg.SocketPath), nil
}

type tableOutput func(io.Writer) error

func writeJSON(cmd *cobra.Command, value any) error {
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(value); err != nil {
		return fmt.Errorf("writing JSON output: %w", err)
	}
	return nil
}

func writeOutput(cmd *cobra.Command, output string, value any, table tableOutput) error {
	switch output {
	case "json":
		return writeJSON(cmd, value)
	case "table":
		return table(cmd.OutOrStdout())
	default:
		return fmt.Errorf("unsupported output format %q", output)
	}
}

func NewCommand() *cobra.Command {
	v := config.NewViper()
	app := &Application{v: v}
	root := &cobra.Command{
		Use:           "artifact",
		Short:         "Create and publish local artifacts",
		Version:       version.Value,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if app.output != "table" && app.output != "json" {
				return fmt.Errorf("unsupported output format %q", app.output)
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&app.configFile, "config", "", "config file")
	root.PersistentFlags().StringVar(&app.output, "output", "table", "output format: table or json")
	if err := config.BindFlags(v, root.PersistentFlags()); err != nil {
		panic(err)
	}
	root.AddCommand(
		createCommand(app),
		previewCommand(app),
		listCommand(app),
		publishCommand(app),
		workspaceCommand(app),
		dataCommand(app),
		editCommand(app),
		versionsCommand(app),
		restoreCommand(app),
		archiveCommand(app),
		unarchiveCommand(app),
		integrationCommand(app),
	)
	return root
}

func createCommand(app *Application) *cobra.Command {
	var id, path, name, description string
	var force, open bool
	command := &cobra.Command{
		Use:   "create [artifact-id]",
		Short: "Create and publish a standalone artifact scaffold",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.loadConfig()
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
			publishedURL := ""
			if result, publishErr := ipc.NewClient(cfg.SocketPath).Publish(cmd.Context(), directory); publishErr == nil {
				publishedURL = result.URL
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "published: %s\n", result.URL); err != nil {
					return fmt.Errorf("writing publish result: %w", err)
				}
				if open {
					if err := openBrowser(cmd.Context(), result.URL); err != nil {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open artifact in browser: %v\n", err)
					}
				}
			} else if !ipc.IsDaemonUnavailable(publishErr) {
				return fmt.Errorf("auto-publishing artifact: %w", publishErr)
			} else {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: artifactd is unavailable; scaffold was not published\n")
			}
			if app.output == "json" {
				return writeJSON(cmd, map[string]any{"directory": filepath.Clean(directory), "published": publishedURL != "", "url": publishedURL})
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

func listCommand(app *Application) *cobra.Command {
	var includeArchived bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List published artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := app.client()
			if err != nil {
				return err
			}
			artifacts, err := client.List(cmd.Context(), includeArchived)
			if err != nil {
				return err
			}
			return writeList(cmd, artifacts, app.output)
		},
	}
	command.Flags().BoolVar(&includeArchived, "include-archived", false, "include archived artifacts")
	return command
}

func publishCommand(app *Application) *cobra.Command {
	var open bool
	command := &cobra.Command{
		Use:   "publish <directory-or-id>",
		Short: "Publish an artifact to artifactd",
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
	return writeOutput(cmd, output, artifacts, func(out io.Writer) error {
		writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
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
	})
}
