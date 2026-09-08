package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const EntryName = "artifactd-markdown.desktop"

func Install(executable string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("desktop integration is currently available only on Linux")
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("resolving artifact executable: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("reading artifact executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("artifact executable must be a regular file")
	}

	directory, err := applicationsDirectory()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("creating applications directory: %w", err)
	}
	content := desktopEntry(absolute)
	path := filepath.Join(directory, EntryName)
	if err := atomicWrite(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing desktop entry: %w", err)
	}
	if err := refreshDatabase(directory); err != nil {
		return "", err
	}
	return path, nil
}

func Uninstall() (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("desktop integration is currently available only on Linux")
	}
	directory, err := applicationsDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, EntryName)
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return path, nil
	} else if err != nil {
		return "", fmt.Errorf("removing desktop entry: %w", err)
	}
	if err := refreshDatabase(directory); err != nil {
		return "", err
	}
	return path, nil
}

func desktopEntry(executable string) string {
	return `[Desktop Entry]
Type=Application
Name=Artifactd Markdown Preview
Comment=Preview Markdown files with Artifactd
Exec=` + quoteExecArg(executable) + ` preview --open --desktop %f
Icon=text-markdown
Terminal=false
NoDisplay=true
MimeType=text/markdown;text/x-markdown;
Categories=Utility;
StartupNotify=true
`
}

func quoteExecArg(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"`", "\\`",
		`$`, `\$`,
		`%`, `%%`,
	)
	return `"` + replacer.Replace(value) + `"`
}

func atomicWrite(path string, content []byte, mode os.FileMode) (resultErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".artifactd-markdown-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("removing temporary desktop entry: %w", removeErr))
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write(content); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func applicationsDirectory() (string, error) {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "applications"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "applications"), nil
}

func refreshDatabase(directory string) error {
	command, err := exec.LookPath("update-desktop-database")
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("finding update-desktop-database: %w", err)
	}
	if output, err := exec.Command(command, directory).CombinedOutput(); err != nil {
		return fmt.Errorf("updating desktop database: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
