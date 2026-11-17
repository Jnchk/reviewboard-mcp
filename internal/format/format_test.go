package format

import (
	"strings"
	"testing"
)

func TestRow(t *testing.T) {
	if got := Row("a", "", "c d"); got != "a | - | c d" {
		t.Fatalf("Row = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("one  two", 10); got != "one two" {
		t.Fatalf("Truncate = %q", got)
	}
	if got := Truncate("abcdef", 3); got != "abc…" {
		t.Fatalf("Truncate = %q", got)
	}
}

func TestClip(t *testing.T) {
	got := Clip("abcdef", 3)
	if !strings.HasPrefix(got, "abc") || !strings.Contains(got, "6 chars total") {
		t.Fatalf("Clip = %q", got)
	}
}

func TestWhen(t *testing.T) {
	if got := When("2026-11-16T09:40:12Z"); got != "2026-11-16 09:40Z" {
		t.Fatalf("When = %q", got)
	}
}
