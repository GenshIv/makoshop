package api

import (
	"bytes"
	"testing"
)

// TestStringToBytes guards the zero-copy string->[]byte conversion.
//
// The old implementation reinterpreted the 16-byte string header (ptr+len) as
// a 24-byte slice header (ptr+len+cap), reading cap from adjacent garbage
// memory. That produced slices with cap < len, which later tripped bounds
// checks in downstream consumers (e.g. crc32 inside gzip), panicking with
// "slice bounds out of range [:N] with capacity M".
func TestStringToBytes(t *testing.T) {
	cases := []string{
		"",
		"a",
		"hello world",
		`<link rel="canonical" href="https://example.com/shop/some/very/long/category/path/that/makes/this/tag/79/bytes/long">`,
		string(make([]byte, 4096)), // larger than any page size
	}
	for _, s := range cases {
		b := stringToBytes(s)
		if len(b) != len(s) {
			t.Fatalf("len mismatch: got %d want %d", len(b), len(s))
		}
		if cap(b) < len(b) {
			t.Fatalf("cap(%d) < len(%d): slice header was under-sized (the old bug)", cap(b), len(b))
		}
		if !bytes.Equal(b, []byte(s)) {
			t.Fatalf("content mismatch for %q", s)
		}
		// The slice must be safely usable up to its full length.
		_ = b[:len(b)]
	}
}

// TestStringToBytesEmpty returns a nil slice for the empty string.
func TestStringToBytesEmpty(t *testing.T) {
	if b := stringToBytes(""); b != nil {
		t.Fatalf("expected nil for empty string, got %v", b)
	}
}
