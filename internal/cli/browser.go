package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

func openBrowser(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parsing artifact URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("artifact URL must be an HTTP or HTTPS URL")
	}

	command, args := browserCommand(rawURL)
	browser := exec.CommandContext(ctx, command, args...)
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
	if err := browser.Run(); err != nil {
		return fmt.Errorf("running %s: %w", command, err)
	}
	return nil
}

func browserCommand(rawURL string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{rawURL}
	case "windows":
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		return "xdg-open", []string{rawURL}
	}
}
