package docx

import (
	"archive/zip"
	"io"
	"strings"
	"testing"
)

func TestRenderCreatesReadableDOCXParts(t *testing.T) {
	content, err := Render([]byte("# Notes\n\nA **useful** paragraph.\n\n- First\n- Second\n\n```text\ncode\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(strings.NewReader(string(content)), int64(len(content)))
	if err != nil {
		t.Fatalf("opening DOCX archive: %v", err)
	}
	var document string
	for _, file := range archive.File {
		if file.Name != "word/document.xml" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		bytes, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("reading DOCX document: %v", readErr)
		}
		document = string(bytes)
	}
	for _, expected := range []string{"Notes", "A useful paragraph.", "First", "Second", "code"} {
		if !strings.Contains(document, expected) {
			t.Errorf("DOCX document missing %q: %s", expected, document)
		}
	}
}
