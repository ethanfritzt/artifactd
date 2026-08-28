package cli

import (
	"context"
	"runtime"
	"testing"
)

func TestOpenBrowserRejectsNonHTTPURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{
			name: "empty URL",
			url:  "",
		},
		{
			name: "file URL",
			url:  "file:///tmp/artifact.html",
		},
		{
			name: "missing host",
			url:  "http:/artifact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := openBrowser(context.Background(), tt.url); err == nil {
				t.Fatalf("openBrowser(%q) returned nil, want validation error", tt.url)
			}
		})
	}
}

func TestBrowserCommand(t *testing.T) {
	command, args := browserCommand("http://example.test/")
	if len(args) != 1 || args[0] != "http://example.test/" {
		t.Fatalf("browserCommand args = %#v, want published URL", args)
	}

	want := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		want = "open"
	case "windows":
		want = "rundll32.exe"
	}
	if command != want {
		t.Fatalf("browserCommand command = %q, want %q", command, want)
	}
}
