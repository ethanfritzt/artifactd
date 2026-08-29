//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package ipc

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWalkFilesRejectsSpecialFiles(t *testing.T) {
	directory := t.TempDir()
	fifo := filepath.Join(directory, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := walkFiles(directory, func(string, string) error { return nil }); err == nil {
		t.Fatal("walkFiles accepted a FIFO")
	}
}

func TestStreamFileRejectsSpecialFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := streamFile(os.Stdout, fifo); err == nil {
		t.Fatal("streamFile accepted a FIFO")
	}
}
