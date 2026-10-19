package canary

import "testing"

func TestDouble(t *testing.T) {
	t.Parallel()
	if got := Double(0); got != 0 {
		t.Fatalf("Double(0) = %d, want 0", got)
	}
}
