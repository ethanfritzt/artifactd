package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallAndUninstall(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux desktop integration")
	}
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	executableDirectory := filepath.Join(t.TempDir(), "artifact tools")
	if err := os.Mkdir(executableDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(executableDirectory, "artifact")
	if err := os.WriteFile(executable, []byte("binary"), 0o750); err != nil {
		t.Fatal(err)
	}

	installed, err := Install(executable)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	entry := string(content)
	if !strings.Contains(entry, `Exec="`+executable+`" preview --open --desktop %f`) {
		t.Fatalf("desktop entry has unexpected Exec line:\n%s", entry)
	}
	if !strings.Contains(entry, "MimeType=text/markdown;text/x-markdown;") {
		t.Fatal("desktop entry does not register Markdown MIME types")
	}

	removed, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed != installed {
		t.Fatalf("Uninstall() path = %q, want %q", removed, installed)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("desktop entry still exists: %v", err)
	}
}

func TestQuoteExecArgEscapesFieldCodes(t *testing.T) {
	quoted := quoteExecArg(`/tmp/artifact % test/$bin`)
	if quoted != `"/tmp/artifact %% test/\$bin"` {
		t.Fatalf("quoteExecArg() = %q", quoted)
	}
}
