package protocol

import (
	"path/filepath"
	"testing"
)

func TestValidateArtifactID(t *testing.T) {
	for _, id := range []string{"demo", "house-budget", "a1"} {
		if err := ValidateArtifactID(id); err != nil {
			t.Errorf("ValidateArtifactID(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"", "Demo", "a--b", "../demo", "a/../b"} {
		if err := ValidateArtifactID(id); err == nil {
			t.Errorf("ValidateArtifactID(%q) = nil", id)
		}
	}
}

func TestValidateDataSource(t *testing.T) {
	if err := ValidateDataSource("metrics-1"); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"Metrics", "metrics/one", "../metrics", ""} {
		if err := ValidateDataSource(source); err == nil {
			t.Errorf("ValidateDataSource(%q) = nil", source)
		}
	}
}

func TestValidateAbsolutePath(t *testing.T) {
	absolute, err := filepath.Abs("artifact")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAbsolutePath(absolute); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "artifact", absolute + string(filepath.Separator) + ".."} {
		if err := ValidateAbsolutePath(path); err == nil {
			t.Errorf("ValidateAbsolutePath(%q) = nil", path)
		}
	}
}
