// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later

package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// ---------------------------------------------------------------------------
// xdrPad
// ---------------------------------------------------------------------------

func TestXdrPad(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{0, 0},
		{1, 3},
		{2, 2},
		{3, 1},
		{4, 0},
		{5, 3},
		{8, 0},
		{13, 3},
		{16, 0},
	}
	for _, tc := range tests {
		got := xdrPad(tc.n)
		if got != tc.want {
			t.Errorf("xdrPad(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// EncodeXDRString
// ---------------------------------------------------------------------------

func TestEncodeXDRString_Empty(t *testing.T) {
	buf := EncodeXDRString("")
	if len(buf) != 4 {
		t.Fatalf("expected 4 bytes for empty string, got %d", len(buf))
	}
	if binary.BigEndian.Uint32(buf[:4]) != 0 {
		t.Fatalf("expected length 0, got %d", binary.BigEndian.Uint32(buf[:4]))
	}
}

func TestEncodeXDRString_AlignedLength(t *testing.T) {
	// "abcd" is 4 bytes, already 4-byte aligned → no padding.
	buf := EncodeXDRString("abcd")
	if len(buf) != 8 { // 4 (len) + 4 (data)
		t.Fatalf("expected 8 bytes, got %d", len(buf))
	}
	if binary.BigEndian.Uint32(buf[:4]) != 4 {
		t.Fatalf("expected length 4, got %d", binary.BigEndian.Uint32(buf[:4]))
	}
	if string(buf[4:8]) != "abcd" {
		t.Fatalf("data mismatch: %q", buf[4:8])
	}
}

func TestEncodeXDRString_UnalignedLength(t *testing.T) {
	// "hello" is 5 bytes → 3 bytes of padding.
	buf := EncodeXDRString("hello")
	expectedLen := 4 + 5 + 3 // len + data + padding
	if len(buf) != expectedLen {
		t.Fatalf("expected %d bytes, got %d", expectedLen, len(buf))
	}
	if binary.BigEndian.Uint32(buf[:4]) != 5 {
		t.Fatalf("expected length 5, got %d", binary.BigEndian.Uint32(buf[:4]))
	}
	if string(buf[4:9]) != "hello" {
		t.Fatalf("data mismatch: %q", buf[4:9])
	}
	// Padding bytes must be zero.
	for i := 9; i < expectedLen; i++ {
		if buf[i] != 0 {
			t.Fatalf("padding byte at offset %d is %d, want 0", i, buf[i])
		}
	}
}

// ---------------------------------------------------------------------------
// DecodeXDRString
// ---------------------------------------------------------------------------

func TestDecodeXDRString_RoundTrip(t *testing.T) {
	inputs := []string{
		"",
		"a",
		"ab",
		"abc",
		"abcd",
		"hello, world!",
		"<domain type='kvm'><name>test</name></domain>",
	}
	for _, input := range inputs {
		encoded := EncodeXDRString(input)
		decoded, consumed, err := DecodeXDRString(encoded)
		if err != nil {
			t.Fatalf("DecodeXDRString(%q): unexpected error: %v", input, err)
		}
		if decoded != input {
			t.Errorf("DecodeXDRString(%q): got %q", input, decoded)
		}
		if consumed != len(encoded) {
			t.Errorf("DecodeXDRString(%q): consumed %d, want %d", input, consumed, len(encoded))
		}
	}
}

func TestDecodeXDRString_TooShortForLength(t *testing.T) {
	_, _, err := DecodeXDRString([]byte{0, 0})
	if err == nil {
		t.Fatal("expected error for buffer shorter than 4 bytes")
	}
}

func TestDecodeXDRString_TooShortForData(t *testing.T) {
	// Encode length = 100 but provide only 4 + 2 bytes of data.
	buf := make([]byte, 6)
	binary.BigEndian.PutUint32(buf[:4], 100)
	_, _, err := DecodeXDRString(buf)
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestDecodeXDRString_LengthTooLarge(t *testing.T) {
	// Set length > xdrStringMaxLen.
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf[:4], xdrStringMaxLen+1)
	_, _, err := DecodeXDRString(buf)
	if err == nil {
		t.Fatal("expected error for length exceeding max")
	}
}

func TestDecodeXDRString_WithTrailingData(t *testing.T) {
	// Encode "hi" (2 bytes + 2 padding) then append extra bytes.
	encoded := EncodeXDRString("hi")
	trailing := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	data := append(encoded, trailing...)

	decoded, consumed, err := DecodeXDRString(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded != "hi" {
		t.Errorf("got %q, want %q", decoded, "hi")
	}
	if consumed != len(encoded) {
		t.Errorf("consumed %d, want %d", consumed, len(encoded))
	}
	// Verify the trailing data is still there.
	if !bytes.Equal(data[consumed:], trailing) {
		t.Errorf("trailing data mismatch")
	}
}

// ---------------------------------------------------------------------------
// ReplaceXDRString
// ---------------------------------------------------------------------------

func TestReplaceXDRString_SameLength(t *testing.T) {
	original := EncodeXDRString("aaaa")
	result, err := ReplaceXDRString(original, "bbbb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, _, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded != "bbbb" {
		t.Errorf("got %q, want %q", decoded, "bbbb")
	}
}

func TestReplaceXDRString_DifferentLength(t *testing.T) {
	original := EncodeXDRString("short")
	result, err := ReplaceXDRString(original, "a much longer replacement string")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, _, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded != "a much longer replacement string" {
		t.Errorf("got %q, want %q", decoded, "a much longer replacement string")
	}
}

func TestReplaceXDRString_PreservesTrailingData(t *testing.T) {
	// Simulate DomainDefineXMLFlags payload: XDR string + 4-byte flags field.
	xmlStr := "<domain/>"
	flags := []byte{0x00, 0x00, 0x00, 0x01} // flags = 1
	payload := append(EncodeXDRString(xmlStr), flags...)

	newXML := "<domain type='kvm'><name>replaced</name></domain>"
	result, err := ReplaceXDRString(payload, newXML)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Decode the replaced string.
	decoded, consumed, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded != newXML {
		t.Errorf("got %q, want %q", decoded, newXML)
	}

	// The trailing flags must be preserved.
	remainder := result[consumed:]
	if !bytes.Equal(remainder, flags) {
		t.Errorf("trailing flags: got %v, want %v", remainder, flags)
	}
}

func TestReplaceXDRString_InvalidPayload(t *testing.T) {
	// Payload too short to contain a valid XDR string.
	_, err := ReplaceXDRString([]byte{0x00}, "new")
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestReplaceXDRString_EmptyToNonEmpty(t *testing.T) {
	original := EncodeXDRString("")
	result, err := ReplaceXDRString(original, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, _, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded != "hello" {
		t.Errorf("got %q, want %q", decoded, "hello")
	}
}

func TestReplaceXDRString_NonEmptyToEmpty(t *testing.T) {
	original := EncodeXDRString("some content")
	result, err := ReplaceXDRString(original, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, _, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded != "" {
		t.Errorf("got %q, want empty string", decoded)
	}
}
