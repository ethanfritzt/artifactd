package create

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCreatesScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if err := Run(Options{Directory: dir}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"artifact.json", "index.html", "styles.css", "app.js"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	var value map[string]any
	content, err := os.ReadFile(filepath.Join(dir, "artifact.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &value); err != nil {
		t.Fatal(err)
	}
	if value["specVersion"] != float64(1) {
		t.Fatalf("specVersion = %v, want 1", value["specVersion"])
	}
	artifact, ok := value["artifact"].(map[string]any)
	if !ok || artifact["id"] != "demo" {
		t.Fatalf("artifact id = %v, want demo", artifact["id"])
	}
	code, ok := value["code"].(map[string]any)
	if !ok {
		t.Fatalf("code = %v, want object", value["code"])
	}
	stack, ok := code["stack"].(map[string]any)
	if !ok || stack["preset"] != "static-web" {
		t.Fatalf("stack preset = %v, want static-web", stack["preset"])
	}
	runtime, ok := value["runtime"].(map[string]any)
	if !ok || runtime["id"] != "web-static" {
		t.Fatalf("runtime id = %v, want web-static", runtime["id"])
	}
}

func TestRunDoesNotOverwriteByDefault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Run(Options{Directory: dir}); err == nil {
		t.Fatal("Run() succeeded for a non-empty directory")
	}
}
