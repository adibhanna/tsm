package session

import "testing"

func TestOutputFilterExtractsSwitchSequence(t *testing.T) {
	var f outputFilter
	payload := []byte("before" + AttachSwitchSequence("adib") + "after")
	filtered, target := f.Filter(payload)
	if string(filtered) != "beforeafter" {
		t.Fatalf("filtered = %q, want %q", string(filtered), "beforeafter")
	}
	if target != "adib" {
		t.Fatalf("target = %q, want adib", target)
	}
}

func TestOutputFilterHandlesSplitSwitchSequence(t *testing.T) {
	var f outputFilter
	seq := AttachSwitchSequence("adib")
	first := "before" + seq[:10]
	second := seq[10:] + "after"

	filtered, target := f.Filter([]byte(first))
	if string(filtered) != "before" {
		t.Fatalf("filtered = %q, want %q", string(filtered), "before")
	}
	if target != "" {
		t.Fatalf("target = %q, want empty", target)
	}

	filtered, target = f.Filter([]byte(second))
	if string(filtered) != "after" {
		t.Fatalf("filtered = %q, want %q", string(filtered), "after")
	}
	if target != "adib" {
		t.Fatalf("target = %q, want adib", target)
	}
}
