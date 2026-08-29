package ipc

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArtifactRouteRejectsInvalidPathSegments(t *testing.T) {
	server := NewServer(nil, nil, nil, nil)
	tests := []string{
		"/v1/artifacts/Bad",
		"/v1/artifacts/demo/data/metrics/extra",
		"/v1/artifacts/demo/data/not-valid!",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestSafeUploadNameRejectsEscapes(t *testing.T) {
	for _, name := range []string{"", ".", "../artifact.json", "assets/../../artifact.json", "assets\\artifact.json", "/artifact.json", "artifact\x00.json"} {
		if _, err := safeUploadName(name); err == nil {
			t.Errorf("safeUploadName(%q) = nil", name)
		}
	}
	for _, name := range []string{"artifact.json", "assets/chart.svg"} {
		if got, err := safeUploadName(name); err != nil || got != name {
			t.Errorf("safeUploadName(%q) = %q, %v", name, got, err)
		}
	}
}
