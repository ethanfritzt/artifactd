package create

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCreatesReactTemplate(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "react-artifact")
	if err := Run(Options{Directory: directory, ID: "react-demo", Name: "React Demo", Template: ReactTemplate}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "index.html", "src/main.tsx", "src/App.tsx", "public/artifact.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "src/GraphView.tsx")); !os.IsNotExist(err) {
		t.Fatalf("base React template included graph feature: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "public/artifact.json"))
	if err != nil {
		t.Fatal(err)
	}
	var specification struct {
		Code struct {
			Stack struct {
				Preset    string `json:"preset"`
				Framework struct {
					ID string `json:"id"`
				} `json:"framework"`
				UIKit struct {
					ID string `json:"id"`
				} `json:"uiKit"`
			} `json:"stack"`
		} `json:"code"`
		Runtime struct {
			ID string `json:"id"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(content, &specification); err != nil {
		t.Fatal(err)
	}
	if specification.Code.Stack.Preset != "react-mantine" || specification.Code.Stack.Framework.ID != "react" || specification.Code.Stack.UIKit.ID != "mantine" {
		t.Fatalf("React stack = %+v, want react-mantine", specification.Code.Stack)
	}
	if specification.Runtime.ID != "web-static" {
		t.Fatalf("runtime = %q, want web-static", specification.Runtime.ID)
	}
}

func TestRunCreatesReactGraphFeature(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "graph-artifact")
	if err := Run(Options{Directory: directory, ID: "graph-demo", Name: "Graph Demo", Template: ReactTemplate, Features: []string{GraphFeature}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"src/GraphView.tsx", "public/graph.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(directory, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packageJSON struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(content, &packageJSON); err != nil {
		t.Fatal(err)
	}
	if packageJSON.Dependencies["cytoscape"] != "3.34.2" {
		t.Fatalf("cytoscape dependency = %q", packageJSON.Dependencies["cytoscape"])
	}
}

func TestRunRejectsUnsupportedTemplateFeature(t *testing.T) {
	err := Run(Options{Directory: filepath.Join(t.TempDir(), "demo"), ID: "demo", Name: "Demo", Template: StaticTemplate, Features: []string{GraphFeature}})
	if err == nil {
		t.Fatal("Run() accepted a feature for the static template")
	}
}
