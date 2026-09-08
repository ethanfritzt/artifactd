package markdown

import (
	"strings"
	"testing"
)

func TestRenderSupportsGitHubFlavoredMarkdown(t *testing.T) {
	source := []byte("# Notes\n\n- [x] done\n\n| A | B |\n|---|---|\n| 1 | 2 |\n")
	result, err := Render(source, "Notes")
	if err != nil {
		t.Fatal(err)
	}
	body := string(result)
	for _, want := range []string{"<h1 id=\"notes\">Notes</h1>", `type="checkbox"`, "<table>"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered document does not contain %q", want)
		}
	}
}

func TestRenderDisablesUnsafeHTMLAndLinks(t *testing.T) {
	source := []byte("<script>alert('xss')</script>\n\n[unsafe](javascript:alert('xss'))")
	result, err := Render(source, "Unsafe")
	if err != nil {
		t.Fatal(err)
	}
	body := string(result)
	if strings.Contains(body, "<script>") {
		t.Fatal("rendered document contains raw script HTML")
	}
	if strings.Contains(strings.ToLower(body), `href="javascript:`) {
		t.Fatal("rendered document contains a javascript link")
	}
}

func TestRenderEscapesTitle(t *testing.T) {
	result, err := Render([]byte("hello"), `<script>alert(1)</script>`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result), "<title><script>") {
		t.Fatal("rendered document contains an unsafe title")
	}
}

func TestIsFilename(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "README.md", want: true},
		{name: "notes.MARKDOWN", want: true},
		{name: "index.html", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsFilename(tt.name); got != tt.want {
				t.Fatalf("IsFilename(%q) = %t, want %t", tt.name, got, tt.want)
			}
		})
	}
}
