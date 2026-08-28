package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"artifactd/internal/config"
	"artifactd/internal/daemon"
	"artifactd/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	v := config.NewViper()
	var configFile string
	command := &cobra.Command{
		Use:           "artifactd",
		Short:         "Run the local artifact daemon",
		Version:       version.Value,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(v, configFile)
			if err != nil {
				return err
			}
			configureLogging(cfg.LogLevel)
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := d.Close(); closeErr != nil {
					slog.Error("closing daemon", "error", closeErr)
				}
			}()
			return d.Run(ctx)
		},
	}
	command.PersistentFlags().StringVar(&configFile, "config", "", "config file")
	if err := config.BindFlags(v, command.PersistentFlags()); err != nil {
		panic(err)
	}
	return command
}

func configureLogging(value string) {
	level := slog.LevelInfo
	switch value {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}
