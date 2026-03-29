package pdf

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"io/ioutil"
	"testing"
)

func TestApplyFilterLZWDecode(t *testing.T) {
	input := "BT /F0 12 Tf (Hello World) Tj ET"

	// Encode with LZW MSB, litWidth=8 (same params PDF uses)
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	w.Write([]byte(input))
	w.Close()

	rd := applyFilter(bytes.NewReader(buf.Bytes()), "LZWDecode", Value{})
	got, err := ioutil.ReadAll(rd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestApplyFilterLZWDecodeTruncated(t *testing.T) {
	input := "BT /F0 12 Tf (Hello World) Tj ET"

	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	w.Write([]byte(input))
	w.Close()

	// Truncate the stream to simulate a PDF without a proper EOD marker
	encoded := buf.Bytes()
	truncated := encoded[:len(encoded)-2]

	rd := applyFilter(bytes.NewReader(truncated), "LZWDecode", Value{})
	got, err := ioutil.ReadAll(rd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected partial data from truncated LZW stream, got nothing")
	}
	if !bytes.HasPrefix([]byte(input), got) {
		t.Errorf("decoded data %q is not a prefix of expected %q", got, input)
	}
}

func TestApplyFilterLZWDecodeEmptyPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for completely invalid LZW data")
		}
	}()

	garbage := []byte{0xFF, 0xFE, 0xFD}
	applyFilter(bytes.NewReader(garbage), "LZWDecode", Value{})
}

func TestApplyFilterUnknownPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unknown filter")
		}
	}()

	applyFilter(bytes.NewReader(nil), "BogusFilter", Value{})
}

func TestApplyFilterFlateDecode(t *testing.T) {
	input := "BT /F0 12 Tf (Hello Flate) Tj ET"

	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write([]byte(input))
	zw.Close()

	rd := applyFilter(bytes.NewReader(buf.Bytes()), "FlateDecode", Value{})
	got, err := ioutil.ReadAll(rd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != input {
		t.Errorf("got %q, want %q", got, input)
	}
}
