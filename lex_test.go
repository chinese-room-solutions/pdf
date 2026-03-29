package pdf

import (
	"strings"
	"testing"
)

// newTestBuffer creates a buffer from a string with settings matching the
// Interpret context (allowEOF=true, allowObjptr=false, allowStream=false).
func newTestBuffer(s string) *buffer {
	b := newBuffer(strings.NewReader(s), 0)
	b.allowEOF = true
	b.allowObjptr = false
	b.allowStream = false
	return b
}

// TestReadArrayClosedNormal verifies a well-formed array parses correctly.
func TestReadArrayClosedNormal(t *testing.T) {
	b := newTestBuffer("[1 2 3]")
	b.readToken() // consume "["
	obj := b.readArray()
	arr, ok := obj.(array)
	if !ok {
		t.Fatalf("expected array, got %T", obj)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
}

// TestReadArrayUnclosedAtEOF is the OOM regression test: an unclosed array
// whose stream ends at EOF must terminate rather than loop forever.
func TestReadArrayUnclosedAtEOF(t *testing.T) {
	// "[" is consumed by readObject which then calls readArray; the array
	// never receives a closing "]" — the stream just ends.
	b := newTestBuffer("[1 2 3")
	b.readToken() // consume "["
	obj := b.readArray()
	arr, ok := obj.(array)
	if !ok {
		t.Fatalf("expected array, got %T", obj)
	}
	// We should get whatever was parseable before EOF.
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements before EOF, got %d", len(arr))
	}
}

// TestReadObjectStrayClosingBracket verifies that a bare "]" seen by
// readObject (e.g. after readArray breaks early on EOF) returns nil
// rather than panicking.
func TestReadObjectStrayClosingBracket(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic on stray ]: %v", r)
		}
	}()
	b := newTestBuffer("]")
	obj := b.readObject()
	if obj != nil {
		t.Fatalf("expected nil for stray ], got %v", obj)
	}
}

// TestReadArrayNestedUnclosed ensures nested unclosed arrays also terminate.
func TestReadArrayNestedUnclosed(t *testing.T) {
	// Outer array is closed; inner array is not.
	b := newTestBuffer("[[1 2 3]")
	b.readToken() // consume outer "["
	// Should not hang or OOM.
	obj := b.readArray()
	arr, ok := obj.(array)
	if !ok {
		t.Fatalf("expected array, got %T", obj)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 inner element, got %d", len(arr))
	}
	inner, ok := arr[0].(array)
	if !ok {
		t.Fatalf("expected inner array, got %T", arr[0])
	}
	if len(inner) != 3 {
		t.Fatalf("expected 3 inner elements, got %d", len(inner))
	}
}

// TestReadArrayEmptyAtEOF ensures an empty unclosed array at EOF returns an
// empty (non-nil) array rather than hanging.
func TestReadArrayEmptyAtEOF(t *testing.T) {
	b := newTestBuffer("[")
	b.readToken() // consume "["
	obj := b.readArray()
	arr, ok := obj.(array)
	if !ok {
		t.Fatalf("expected array, got %T", obj)
	}
	if len(arr) != 0 {
		t.Fatalf("expected empty array, got %d elements", len(arr))
	}
}
