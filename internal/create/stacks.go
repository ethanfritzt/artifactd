package create

import "artifactd/internal/manifest"

// StackDescriptor describes an authoring preset without adding runtime behavior.
type StackDescriptor struct {
	ID    string
	Stack manifest.StackSpec
}

var stackDescriptors = map[string]StackDescriptor{
	StaticTemplate: {
		ID: "static-web",
		Stack: manifest.StackSpec{
			Preset:    "static-web",
			Target:    "browser",
			Languages: []string{"html", "css", "javascript"},
			Build: &manifest.BuildSpec{
				Type:   "none",
				Output: "static",
			},
		},
	},
	ReactTemplate: {
		ID: "react-mantine",
		Stack: manifest.StackSpec{
			Preset:    "react-mantine",
			Target:    "browser",
			Languages: []string{"typescript", "tsx", "css"},
			Framework: &manifest.TechnologySpec{ID: "react", Version: "19"},
			UIKit:     &manifest.TechnologySpec{ID: "mantine", Version: "9"},
			Build: &manifest.BuildSpec{
				Type:    "bundler",
				Tool:    "vite",
				Version: "8",
				Output:  "static",
			},
		},
	},
}

func stackForTemplate(template string) manifest.StackSpec {
	descriptor, ok := stackDescriptors[template]
	if !ok {
		return manifest.StackSpec{}
	}
	return descriptor.Stack
}
