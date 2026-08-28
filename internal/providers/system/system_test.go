package system

import "testing"

func TestProviderSnapshot(t *testing.T) {
	provider := NewProvider()
	first, err := provider.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.Memory.TotalBytes == 0 || second.Memory.TotalBytes == 0 {
		t.Fatal("snapshot returned no memory total")
	}
	if len(second.Processes) == 0 {
		t.Fatal("snapshot returned no processes")
	}
	if second.CPU.Percent < 0 || second.CPU.Percent > 100 {
		t.Fatalf("CPU percent = %v", second.CPU.Percent)
	}
}
