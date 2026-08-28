package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"artifactd/internal/manifest"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 7337
)

type Config struct {
	DataDir         string
	SocketPath      string
	DefaultArtifact string
	Host            string
	PublicHost      string
	Port            int
	LogLevel        string
}

func NewViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("ARTIFACTD")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	v.SetDefault("host", DefaultHost)
	v.SetDefault("public-host", "artifacts.localhost")
	v.SetDefault("port", DefaultPort)
	v.SetDefault("log-level", "info")
	return v
}

func BindFlags(v *viper.Viper, flags *pflag.FlagSet) error {
	flags.String("data-dir", "", "artifactd data directory")
	flags.String("socket", "", "artifactd Unix socket path")
	flags.String("default-artifact", "", "default artifact source directory")
	flags.String("host", DefaultHost, "HTTP listen host")
	flags.String("public-host", "artifacts.localhost", "host used in published URLs")
	flags.Int("port", DefaultPort, "HTTP listen port")
	flags.String("log-level", "info", "log level")

	for _, name := range []string{"data-dir", "socket", "default-artifact", "host", "public-host", "port", "log-level"} {
		if err := v.BindPFlag(name, flags.Lookup(name)); err != nil {
			return fmt.Errorf("binding %s: %w", name, err)
		}
	}
	return nil
}

func Load(v *viper.Viper, configFile string) (Config, error) {
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			v.AddConfigPath(home)
		}
		v.AddConfigPath(".")
		v.SetConfigName(".artifactd")
		v.SetConfigType("yaml")
	}
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, fmt.Errorf("reading configuration: %w", err)
		}
	}

	dataDir := v.GetString("data-dir")
	if dataDir == "" {
		var err error
		dataDir, err = defaultDataDir()
		if err != nil {
			return Config{}, err
		}
	}
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolving data directory: %w", err)
	}

	socketPath := v.GetString("socket")
	if socketPath == "" {
		socketPath = defaultSocketPath(dataDir)
	}

	port := v.GetInt("port")
	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("port must be between 1 and 65535")
	}

	defaultArtifact := v.GetString("default-artifact")
	if defaultArtifact != "" {
		defaultArtifact, err = filepath.Abs(defaultArtifact)
		if err != nil {
			return Config{}, fmt.Errorf("resolving default artifact: %w", err)
		}
	}

	return Config{
		DataDir:         dataDir,
		SocketPath:      socketPath,
		DefaultArtifact: defaultArtifact,
		Host:            v.GetString("host"),
		PublicHost:      v.GetString("public-host"),
		Port:            port,
		LogLevel:        v.GetString("log-level"),
	}, nil
}

// ManagedSourcePath returns the daemon-owned editable source directory for an artifact.
func ManagedSourcePath(dataDir, artifactID string) (string, error) {
	if !manifest.ValidID(artifactID) {
		return "", fmt.Errorf("invalid artifact ID %q", artifactID)
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolving data directory: %w", err)
	}
	return filepath.Join(root, "sources", artifactID), nil
}

func defaultDataDir() (string, error) {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "artifactd"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "artifactd"), nil
}

func defaultSocketPath(dataDir string) string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "artifactd.sock")
	}
	return filepath.Join(dataDir, "artifactd.sock")
}
