// Copyright 2026 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
)

// buildTestPDF constructs a minimal single-page PDF with a custom
// Differences encoding where byte 0x0a (LF) maps to the "three" glyph.
// The content stream uses TJ with one glyph "P". Before the fix, the
// TJ handler would append a spurious showText("\n"), which the
// dictEncoder rendered as "3", producing output like "P3".
func buildTestPDF(t *testing.T) []byte {
	t.Helper()
	content := "BT /F1 12 Tf 100 700 Td [(P)-50] TJ ET\n"
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	contentBytes := zbuf.Bytes()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	offsets := []int{}
	record := func() { offsets = append(offsets, buf.Len()) }

	record()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	record()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	record()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n")
	record()
	// Font Encoding dict with Differences that maps code 10 to /three.
	// This reproduces the trigger: sending "\n" (byte 10) through the encoder
	// would yield '3'.
	buf.WriteString("4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [10 /three] >> >>\nendobj\n")
	record()
	fmt.Fprintf(&buf, "5 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", len(contentBytes))
	buf.Write(contentBytes)
	buf.WriteString("\nendstream\nendobj\n")

	xrefOff := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xrefOff)
	return buf.Bytes()
}

// TestTJNoSpuriousNewline verifies that the TJ operator does not emit a
// trailing newline through the font encoder, which previously caused
// fonts with a /three glyph at code 10 to produce a spurious "3" after
// every TJ group (e.g. "PUBLICACIÓN" rendered as "P3U3B3L3I3C3A3C3I3Ó3N3").
func TestTJNoSpuriousNewline(t *testing.T) {
	data := buildTestPDF(t)
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	p := r.Page(1)
	if p.V.IsNull() {
		t.Fatal("page 1 is null")
	}
	content := p.Content()

	var got strings.Builder
	for _, tx := range content.Text {
		got.WriteString(tx.S)
	}
	out := got.String()

	if strings.Contains(out, "3") {
		t.Errorf("TJ emitted spurious digit: got %q, want no '3'", out)
	}
	if !strings.Contains(out, "P") {
		t.Errorf("expected 'P' in output, got %q", out)
	}
}
