package markdown

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

const MaxBytes = 5 << 20

// Stylesheet is the daemon-owned presentation for rendered Markdown documents.
const Stylesheet = `:root {
  color-scheme: light dark;
  font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-synthesis: none;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  color: CanvasText;
  background: Canvas;
  font-size: 16px;
  line-height: 1.65;
}
.artifactd-markdown {
  width: min(100% - 2rem, 52rem);
  margin: 0 auto;
  padding: 4.5rem 0 6rem;
}
h1, h2, h3, h4, h5, h6 {
  margin: 2em 0 .65em;
  line-height: 1.2;
  letter-spacing: -.018em;
}
h1 { margin-top: 0; font-size: clamp(2rem, 5vw, 3.25rem); }
h2 { padding-bottom: .3em; border-bottom: 1px solid color-mix(in srgb, CanvasText 18%, transparent); }
p, ul, ol, blockquote, pre, table { margin: 1em 0; }
a { color: LinkText; text-underline-offset: .18em; }
a:hover { text-decoration-thickness: .12em; }
img { max-width: 100%; height: auto; border-radius: .35rem; }
blockquote {
  margin-left: 0;
  padding: .1rem 1rem;
  border-left: .25rem solid color-mix(in srgb, AccentColor 65%, CanvasText);
  color: color-mix(in srgb, CanvasText 72%, Canvas);
}
code, pre { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
code {
  padding: .12em .35em;
  border-radius: .3rem;
  background: color-mix(in srgb, CanvasText 8%, Canvas);
  font-size: .9em;
}
pre {
  overflow-x: auto;
  padding: 1rem 1.1rem;
  border: 1px solid color-mix(in srgb, CanvasText 14%, transparent);
  border-radius: .55rem;
  background: color-mix(in srgb, CanvasText 6%, Canvas);
}
pre code { padding: 0; background: transparent; }
table { width: 100%; border-collapse: collapse; }
th, td { padding: .55rem .7rem; border: 1px solid color-mix(in srgb, CanvasText 18%, transparent); text-align: left; }
th { background: color-mix(in srgb, CanvasText 7%, Canvas); }
hr { margin: 2rem 0; border: 0; border-top: 1px solid color-mix(in srgb, CanvasText 20%, transparent); }
input[type="checkbox"] { margin-right: .45em; }
@media (max-width: 36rem) {
  .artifactd-markdown { width: min(100% - 1.25rem, 52rem); padding-top: 4rem; }
}
@media print {
  .artifactd-markdown { width: 100%; padding: 0; }
}
`

func IsFilename(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".md" || extension == ".markdown"
}

// RenderBody renders a safe HTML fragment using the same engine as full documents.
func RenderBody(source []byte) ([]byte, error) {
	if len(source) > MaxBytes {
		return nil, fmt.Errorf("markdown document exceeds %d bytes", MaxBytes)
	}

	engine := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	var body bytes.Buffer
	if err := engine.Convert(source, &body); err != nil {
		return nil, fmt.Errorf("rendering markdown: %w", err)
	}

	return body.Bytes(), nil
}

func Render(source []byte, title string) ([]byte, error) {
	body, err := RenderBody(source)
	if err != nil {
		return nil, err
	}

	var document bytes.Buffer
	document.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	document.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>")
	document.WriteString(stdhtml.EscapeString(title))
	document.WriteString("</title>\n<link rel=\"stylesheet\" href=\"/_artifactd/markdown.css\">\n</head>\n<body>\n<main class=\"artifactd-markdown\">\n")
	// Goldmark escapes raw HTML and dangerous link destinations unless its
	// unsafe renderer option is explicitly enabled. Artifactd never enables it.
	document.Write(body)
	document.WriteString("</main>\n</body>\n</html>\n")
	return document.Bytes(), nil
}
