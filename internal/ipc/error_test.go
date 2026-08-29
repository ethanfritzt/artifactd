package ipc

import (
	"bytes"
	"errors"
	"testing"
)

func TestDecodeErrorIsTyped(t *testing.T) {
	err := decodeError(bytes.NewBufferString(`{"error":"artifact not found"}`), "404 Not Found", 404)
	var daemonErr *DaemonError
	if !errors.As(err, &daemonErr) {
		t.Fatalf("error = %T, want DaemonError", err)
	}
	if daemonErr.StatusCode != 404 || daemonErr.Message != "artifact not found" {
		t.Fatalf("daemon error = %+v", daemonErr)
	}
}
