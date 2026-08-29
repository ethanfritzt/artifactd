package create

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"artifactd/internal/manifest"
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

type Options struct {
	Directory   string
	ID          string
	Name        string
	Description string
	Force       bool
}

func Run(options Options) error {
	if options.Directory == "" {
		return fmt.Errorf("directory is required")
	}
	if options.ID == "" {
		options.ID = slug(filepath.Base(filepath.Clean(options.Directory)))
	}
	if options.Name == "" {
		options.Name = displayName(filepath.Base(filepath.Clean(options.Directory)))
	}
	if options.Description == "" {
		options.Description = "A standalone artifact"
	}

	if err := validateOptions(options); err != nil {
		return err
	}
	created := false
	if err := os.Mkdir(options.Directory, 0o750); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("creating artifact directory: %w", err)
		}
		entries, readErr := os.ReadDir(options.Directory)
		if readErr != nil {
			return fmt.Errorf("checking artifact directory: %w", readErr)
		}
		if len(entries) > 0 && !options.Force {
			return fmt.Errorf("artifact directory is not empty: %s", options.Directory)
		}
	} else {
		created = true
	}

	manifestContent, err := manifestJSON(options)
	if err != nil {
		return fmt.Errorf("encoding artifact manifest: %w", err)
	}
	files := map[string][]byte{
		"artifact.json": manifestContent,
		"index.html":    []byte(indexHTML(options.Name)),
		"styles.css":    []byte(stylesCSS()),
		"app.js":        []byte(appJS()),
	}
	for name, content := range files {
		path := filepath.Join(options.Directory, name)
		if _, err := os.Stat(path); err == nil && !options.Force {
			if created {
				if removeErr := os.RemoveAll(options.Directory); removeErr != nil {
					return fmt.Errorf("cleaning up artifact directory: %w", removeErr)
				}
			}
			return fmt.Errorf("refusing to overwrite %s", path)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("checking %s: %w", path, err)
		}
		if err := os.WriteFile(path, content, 0o640); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func validateOptions(options Options) error {
	if !idPattern.MatchString(options.ID) || len(options.ID) > 63 {
		return fmt.Errorf("id must contain lowercase letters, numbers, and single hyphens")
	}
	if strings.TrimSpace(options.Name) == "" {
		return fmt.Errorf("name cannot be empty")
	}
	return nil
}

func slug(value string) string {
	value = strings.ToLower(value)
	value = slugPattern.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func displayName(value string) string {
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	words := strings.Fields(value)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

func manifestJSON(options Options) ([]byte, error) {
	value := struct {
		SpecVersion int `json:"specVersion"`
		Artifact    struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		} `json:"artifact"`
		Code struct {
			Format string `json:"format"`
			Entry  string `json:"entry"`
		} `json:"code"`
		Runtime      manifest.RuntimeSpec `json:"runtime"`
		Capabilities []string             `json:"capabilities"`
	}{
		SpecVersion: manifest.CurrentVersion,
		Code: struct {
			Format string `json:"format"`
			Entry  string `json:"entry"`
		}{
			Format: "files",
			Entry:  "index.html",
		},
		Runtime:      manifest.RuntimeSpec{ID: manifest.DefaultRuntimeID, Version: manifest.CurrentRuntimeVersion},
		Capabilities: []string{},
	}
	value.Artifact.ID = options.ID
	value.Artifact.Name = options.Name
	value.Artifact.Description = options.Description

	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func indexHTML(name string) string {
	name = html.EscapeString(name)
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <link rel="stylesheet" href="styles.css">
</head>
<body>
  <main>
    <h1>%s</h1>
    <p>Your artifact is ready.</p>
  </main>
  <script src="app.js"></script>
</body>
</html>
`, name, name)
}

func stylesCSS() string {
	return `:root {
  color-scheme: light;
  font-family: "Avenir Next", Avenir, "Segoe UI", sans-serif;
  color: #25231f;
  background: #eee9df;
  --accent: #c45436;
  --paper: #fffdf8;
  --muted: #746f67;
}

* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 2rem;
  background:
    radial-gradient(circle at 15% 10%, #ffffffaa, transparent 32%),
    linear-gradient(135deg, #eee9df, #e5dfd4);
}
main {
  width: min(100%, 42rem);
  padding: clamp(2rem, 7vw, 4.5rem);
  background: var(--paper);
  border: 1px solid #25231f18;
  border-radius: 1.5rem;
  box-shadow: 0 1.5rem 4rem #4d40301f;
}
h1 {
  max-width: 12ch;
  margin: 0 0 1rem;
  font-size: clamp(2.5rem, 8vw, 5rem);
  line-height: .92;
  letter-spacing: -.06em;
}
p {
  max-width: 34rem;
  margin: 0;
  color: var(--muted);
  font-size: 1.05rem;
  line-height: 1.6;
}

@media (max-width: 32rem) {
  body { padding: 1rem; }
  main { border-radius: 1rem; }
}
`
}

func appJS() string {
	return `console.log("artifact loaded");
`
}
