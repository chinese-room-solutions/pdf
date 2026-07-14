// Copyright 2026 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math"
	"strings"
	"testing"
)

// buildFontPDF constructs a minimal single-page PDF whose page references
// /F1 = object 4; fontObjs are written as objects 4, 5, ... (so a Type0
// font's descendant can be passed as a second element and referenced as
// "5 0 R"). The content stream becomes the object after the fonts.
func buildFontPDF(t *testing.T, fontObjs []string, content string) []byte {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	contentBytes := zbuf.Bytes()
	contentNum := 4 + len(fontObjs)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	offsets := []int{}
	record := func() { offsets = append(offsets, buf.Len()) }

	record()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	record()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	record()
	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents %d 0 R >>\nendobj\n", contentNum)
	for i, obj := range fontObjs {
		record()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", 4+i, obj)
	}
	record()
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", contentNum, len(contentBytes))
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

// buildTestPDF constructs a minimal single-page PDF with a custom
// Differences encoding where byte 0x0a (LF) maps to the "three" glyph.
// The content stream uses TJ with one glyph "P". Before the fix, the
// TJ handler would append a spurious showText("\n"), which the
// dictEncoder rendered as "3", producing output like "P3".
func buildTestPDF(t *testing.T) []byte {
	t.Helper()
	// Font Encoding dict with Differences that maps code 10 to /three.
	// This reproduces the trigger: sending "\n" (byte 10) through the encoder
	// would yield '3'.
	return buildFontPDF(t,
		[]string{"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [10 /three] >> >>"},
		"BT /F1 12 Tf 100 700 Td [(P)-50] TJ ET\n")
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

// checkGlyphs asserts each extracted fragment's width and X position.
// Regression coverage for two width-calculation bugs: a fixed 2-byte code
// stride that made every simple-font glyph width 0 (and stopped the text
// matrix advancing), and Content()'s font resolution dropping the CID
// width table so composite-font glyphs got width 0 too.
func checkGlyphs(t *testing.T, data []byte, wantW, wantX []float64) {
	t.Helper()
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	texts := r.Page(1).Content().Text
	if len(texts) != len(wantW) {
		t.Fatalf("got %d fragments, want %d: %v", len(texts), len(wantW), texts)
	}
	const eps = 1e-6
	for i, tx := range texts {
		if math.Abs(tx.W-wantW[i]) > eps {
			t.Errorf("fragment %d: W = %v, want %v", i, tx.W, wantW[i])
		}
		if math.Abs(tx.X-wantX[i]) > eps {
			t.Errorf("fragment %d: X = %v, want %v", i, tx.X, wantX[i])
		}
	}
}

func TestSimpleFontWidths(t *testing.T) {
	data := buildFontPDF(t,
		[]string{"<< /Type /Font /Subtype /TrueType /BaseFont /Arial /FirstChar 65 /LastChar 67 /Widths [700 710 720] >>"},
		"BT /F1 10 Tf 100 700 Td (ABC) Tj ET\n")
	// Glyph widths come from /Widths scaled by font size /1000; each X
	// starts where the previous glyph's advance ended.
	checkGlyphs(t, data,
		[]float64{7.0, 7.1, 7.2},
		[]float64{100, 107, 114.1})
}

func TestCIDFontWidths(t *testing.T) {
	data := buildFontPDF(t,
		[]string{
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [5 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 500 /W [65 [700 710]] >>",
		},
		"BT /F1 10 Tf 100 700 Td <004100420043> Tj ET\n")
	// Codes are 2 bytes each: 0x41 and 0x42 hit the /W table, 0x43 falls
	// back to /DW.
	checkGlyphs(t, data,
		[]float64{7.0, 7.1, 5.0},
		[]float64{100, 107, 114.1})
}

// TestToUnicodeStubFallback: a ToUnicode CMap only covers the codes it maps.
// Word/Ghostscript emit stub CMaps mapping a handful of codes; the rest must
// fall back per-code to the /Encoding-derived encoder instead of decoding to
// U+FFFD (which downstream cleanup strips, silently deleting page text).
func TestToUnicodeStubFallback(t *testing.T) {
	cmapStream := `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CMapType 2 def
1 begincodespacerange
<00><ff>
endcodespacerange
1 beginbfrange
<41><41><005a>
endbfrange
endcmap
CMapName currentdict /CMap defineresource pop
end end`
	data := buildFontPDF(t,
		[]string{
			"<< /Type /Font /Subtype /TrueType /BaseFont /Arial /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 66 /Widths [700 700] /ToUnicode 5 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmapStream), cmapStream),
		},
		"BT /F1 10 Tf 100 700 Td (AB) Tj ET\n")
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	var got strings.Builder
	for _, tx := range r.Page(1).Content().Text {
		got.WriteString(tx.S)
	}
	// 'A' (0x41) goes through the CMap's bfrange (→ 'Z'); 'B' (0x42) is
	// unmapped there and must fall back to WinAnsi.
	if got.String() != "ZB" {
		t.Errorf("got %q, want %q", got.String(), "ZB")
	}
}
