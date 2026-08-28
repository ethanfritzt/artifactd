package runtime

import (
	"testing"
)

func TestStorePutAndGet(t *testing.T) {
	store := NewStore()
	if _, err := store.Put("demo", "metrics", []byte(`{"value":42}`)); err != nil {
		t.Fatal(err)
	}
	data, err := store.Get("demo", "metrics")
	if err != nil {
		t.Fatal(err)
	}
	if string(data.Data) != `{"value":42}` {
		t.Fatalf("data = %s", data.Data)
	}
	data.Data[0] = '{'
	stored, err := store.Get("demo", "metrics")
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Data) != `{"value":42}` {
		t.Fatalf("store returned aliased data: %s", stored.Data)
	}
}

func TestStoreRejectsInvalidSourceAndJSON(t *testing.T) {
	store := NewStore()
	tests := []struct {
		name   string
		source string
		data   string
	}{
		{name: "invalid source", source: "../metrics", data: `{}`},
		{name: "invalid json", source: "metrics", data: "not json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.Put("demo", tt.source, []byte(tt.data)); err == nil {
				t.Fatal("Put() accepted invalid input")
			}
		})
	}
	if _, err := store.Get("demo", "missing"); err != ErrNotFound {
		t.Fatalf("Get() error = %v, want %v", err, ErrNotFound)
	}
}
