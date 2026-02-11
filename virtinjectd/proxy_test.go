// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later

package main

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"
)

// makeHookChain is a test helper that creates a HookChain with a single
// executable hook script whose body is the given shell code.
func makeHookChain(t *testing.T, scriptBody string) *HookChain {
	t.Helper()
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, scriptBody)
	hc, err := NewHookChain(testLogger(), hookFile, "", "")
	if err != nil {
		t.Fatalf("NewHookChain: %v", err)
	}
	return hc
}

// makeXDRPayload builds a minimal DomainDefineXML-style payload:
// an XDR-encoded string (the domain XML).
func makeXDRPayload(xml string) []byte {
	return EncodeXDRString(xml)
}

// makeXDRPayloadWithFlags builds a DomainDefineXMLFlags-style payload:
// an XDR-encoded string followed by a 4-byte big-endian flags field.
func makeXDRPayloadWithFlags(xml string, flags uint32) []byte {
	encoded := EncodeXDRString(xml)
	flagsBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(flagsBuf, flags)
	return append(encoded, flagsBuf...)
}

// decodePayloadXML is a test helper that decodes the XDR string from a payload.
func decodePayloadXML(t *testing.T, payload []byte) string {
	t.Helper()
	s, _, err := DecodeXDRString(payload)
	if err != nil {
		t.Fatalf("DecodeXDRString: %v", err)
	}
	return s
}

// ---------------------------------------------------------------------------
// patchDefineXML: invalid payload (DecodeXDRString fails)
// ---------------------------------------------------------------------------

func TestPatchDefineXML_InvalidPayload(t *testing.T) {
	hooks := makeHookChain(t, "cat\n")

	// Payload too short to contain a valid XDR string.
	badPayload := []byte{0x00, 0x01}
	result, err := patchDefineXML(testLogger(), badPayload, hooks)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
	// Original payload must be returned on error.
	if !bytes.Equal(result, badPayload) {
		t.Errorf("expected original payload on error, got different bytes")
	}
}

func TestPatchDefineXML_EmptyPayload(t *testing.T) {
	hooks := makeHookChain(t, "cat\n")

	result, err := patchDefineXML(testLogger(), nil, hooks)
	if err == nil {
		t.Fatal("expected error for nil payload")
	}
	if result != nil {
		t.Errorf("expected nil result for nil payload, got %d bytes", len(result))
	}
}

// ---------------------------------------------------------------------------
// patchDefineXML: hooks pass XML through unchanged
// ---------------------------------------------------------------------------

func TestPatchDefineXML_Passthrough(t *testing.T) {
	// Hook that copies stdin to stdout unchanged.
	hooks := makeHookChain(t, "cat\n")

	xml := "<domain type='kvm'><name>test-vm</name></domain>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Payload should be identical when hooks don't modify.
	if !bytes.Equal(result, payload) {
		t.Errorf("expected identical payload on passthrough")
	}
}

func TestPatchDefineXML_PassthroughEmptyStdout(t *testing.T) {
	// Hook that produces no output → treated as "no changes".
	hooks := makeHookChain(t, "true\n")

	xml := "<domain><name>vm</name></domain>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(result, payload) {
		t.Errorf("expected identical payload when hook produces empty output")
	}
}

// ---------------------------------------------------------------------------
// patchDefineXML: hooks modify XML
// ---------------------------------------------------------------------------

func TestPatchDefineXML_HookModifiesXML(t *testing.T) {
	// Hook that replaces the XML entirely.
	hooks := makeHookChain(t, `echo "<domain type='kvm'><name>patched</name></domain>"`)

	xml := "<domain type='kvm'><name>original</name></domain>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The result should differ from the original payload.
	if bytes.Equal(result, payload) {
		t.Fatal("expected payload to be modified")
	}

	// Decode and check the patched XML.
	got := decodePayloadXML(t, result)
	expected := "<domain type='kvm'><name>patched</name></domain>\n"
	if got != expected {
		t.Errorf("patched XML = %q, want %q", got, expected)
	}
}

func TestPatchDefineXML_HookAppendsContent(t *testing.T) {
	// Hook that reads stdin and appends extra XML content.
	hooks := makeHookChain(t, `
input=$(cat)
echo "${input}<!-- injected by hook -->"
`)

	xml := "<domain/>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := decodePayloadXML(t, result)
	expected := "<domain/><!-- injected by hook -->\n"
	if got != expected {
		t.Errorf("patched XML = %q, want %q", got, expected)
	}
}

func TestPatchDefineXML_HookShrinsXML(t *testing.T) {
	// Hook that returns shorter XML than the input.
	hooks := makeHookChain(t, `echo "<d/>"`)

	xml := "<domain type='kvm'><name>a-very-long-domain-name-that-gets-replaced</name><memory>1048576</memory></domain>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := decodePayloadXML(t, result)
	if got != "<d/>\n" {
		t.Errorf("patched XML = %q, want %q", got, "<d/>\n")
	}

	// Result payload should be smaller.
	if len(result) >= len(payload) {
		t.Errorf("expected result (%d bytes) to be smaller than original (%d bytes)",
			len(result), len(payload))
	}
}

func TestPatchDefineXML_HookGrowsXML(t *testing.T) {
	// Hook that returns much longer XML.
	hooks := makeHookChain(t, `echo "<domain type='kvm'><name>vm</name><memory unit='KiB'>1048576</memory><vcpu>4</vcpu></domain>"`)

	xml := "<d/>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := decodePayloadXML(t, result)
	expected := "<domain type='kvm'><name>vm</name><memory unit='KiB'>1048576</memory><vcpu>4</vcpu></domain>\n"
	if got != expected {
		t.Errorf("patched XML = %q, want %q", got, expected)
	}

	// Result payload should be larger.
	if len(result) <= len(payload) {
		t.Errorf("expected result (%d bytes) to be larger than original (%d bytes)",
			len(result), len(payload))
	}
}

// ---------------------------------------------------------------------------
// patchDefineXML: hook failure
// ---------------------------------------------------------------------------

func TestPatchDefineXML_HookFails(t *testing.T) {
	// Hook that exits with non-zero → Run logs the error but returns
	// the original XML. Since patchedXML == xmlStr, patchDefineXML
	// returns the original payload.
	hooks := makeHookChain(t, "exit 1\n")

	xml := "<domain><name>keep-me</name></domain>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The original payload is returned because Run returns the
	// original XML on hook failure (error is logged, not propagated).
	if !bytes.Equal(result, payload) {
		t.Errorf("expected original payload preserved on hook failure")
	}
}

// ---------------------------------------------------------------------------
// patchDefineXML: DomainDefineXMLFlags (payload with trailing flags)
// ---------------------------------------------------------------------------

func TestPatchDefineXML_PreservesFlags_Passthrough(t *testing.T) {
	hooks := makeHookChain(t, "cat\n")

	xml := "<domain type='kvm'><name>flagged</name></domain>"
	flags := uint32(0x00000003)
	payload := makeXDRPayloadWithFlags(xml, flags)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// On passthrough, the entire payload (including flags) is unchanged.
	if !bytes.Equal(result, payload) {
		t.Errorf("expected identical payload on passthrough with flags")
	}
}

func TestPatchDefineXML_PreservesFlags_Modified(t *testing.T) {
	hooks := makeHookChain(t, `echo "<domain type='kvm'><name>modified</name></domain>"`)

	xml := "<domain type='kvm'><name>original</name></domain>"
	flags := uint32(0x00000007)
	payload := makeXDRPayloadWithFlags(xml, flags)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Decode the XML from the result.
	got := decodePayloadXML(t, result)
	expected := "<domain type='kvm'><name>modified</name></domain>\n"
	if got != expected {
		t.Errorf("patched XML = %q, want %q", got, expected)
	}

	// The trailing 4 bytes must be the original flags.
	gotStr, consumed, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("DecodeXDRString: %v", err)
	}
	_ = gotStr

	remainder := result[consumed:]
	if len(remainder) != 4 {
		t.Fatalf("expected 4 bytes of trailing flags, got %d", len(remainder))
	}
	gotFlags := binary.BigEndian.Uint32(remainder)
	if gotFlags != flags {
		t.Errorf("flags = %#x, want %#x", gotFlags, flags)
	}
}

func TestPatchDefineXML_PreservesFlags_HookShrinks(t *testing.T) {
	// Hook returns much shorter XML — flags must still be preserved.
	hooks := makeHookChain(t, `echo "<d/>"`)

	xml := "<domain type='kvm'><name>long-name-here</name><memory>2097152</memory></domain>"
	flags := uint32(0xCAFEBABE)
	payload := makeXDRPayloadWithFlags(xml, flags)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, consumed, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("DecodeXDRString: %v", err)
	}

	remainder := result[consumed:]
	if len(remainder) != 4 {
		t.Fatalf("expected 4 bytes of trailing flags, got %d", len(remainder))
	}
	gotFlags := binary.BigEndian.Uint32(remainder)
	if gotFlags != flags {
		t.Errorf("flags = %#x, want %#x", gotFlags, flags)
	}
}

func TestPatchDefineXML_PreservesFlags_HookGrows(t *testing.T) {
	// Hook returns much longer XML — flags must still be preserved.
	hooks := makeHookChain(t, `echo "<domain type='kvm'><name>vm</name><memory unit='KiB'>1048576</memory><vcpu>4</vcpu><devices><emulator>/usr/bin/qemu-system-x86_64</emulator></devices></domain>"`)

	xml := "<d/>"
	flags := uint32(0x00000001)
	payload := makeXDRPayloadWithFlags(xml, flags)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, consumed, err := DecodeXDRString(result)
	if err != nil {
		t.Fatalf("DecodeXDRString: %v", err)
	}

	remainder := result[consumed:]
	if len(remainder) != 4 {
		t.Fatalf("expected 4 bytes of trailing flags, got %d", len(remainder))
	}
	gotFlags := binary.BigEndian.Uint32(remainder)
	if gotFlags != flags {
		t.Errorf("flags = %#x, want %#x", gotFlags, flags)
	}
}

// ---------------------------------------------------------------------------
// patchDefineXML: edge cases
// ---------------------------------------------------------------------------

func TestPatchDefineXML_EmptyXML(t *testing.T) {
	// The domain XML is an empty string.
	hooks := makeHookChain(t, `echo "<patched/>"`)

	payload := makeXDRPayload("")

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := decodePayloadXML(t, result)
	if got != "<patched/>\n" {
		t.Errorf("got %q, want %q", got, "<patched/>\n")
	}
}

func TestPatchDefineXML_LargeXML(t *testing.T) {
	// Test with a large-ish XML payload to ensure no size-related issues.
	hooks := makeHookChain(t, "cat\n")

	// Build a ~64 KiB XML string.
	var buf bytes.Buffer
	buf.WriteString("<domain type='kvm'><name>large-vm</name><devices>")
	for i := 0; i < 1000; i++ {
		buf.WriteString("<disk type='file'><source file='/dev/null'/><target dev='vd")
		buf.WriteByte(byte('a' + (i % 26)))
		buf.WriteString("'/></disk>")
	}
	buf.WriteString("</devices></domain>")
	xml := buf.String()

	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Passthrough: payload unchanged.
	if !bytes.Equal(result, payload) {
		t.Error("expected identical payload on passthrough with large XML")
	}

	// Verify the round-trip preserves the full XML.
	got := decodePayloadXML(t, result)
	if got != xml {
		t.Errorf("XML round-trip mismatch: got %d bytes, want %d bytes", len(got), len(xml))
	}
}

func TestPatchDefineXML_XMLWithSpecialChars(t *testing.T) {
	// XML containing characters that might trip up shell or XDR encoding.
	hooks := makeHookChain(t, "cat\n")

	xml := `<domain type='kvm'>
  <name>test's "vm" &amp; stuff</name>
  <description>line1
line2
line3</description>
  <metadata>null bytes are not here but newlines are</metadata>
</domain>`

	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := decodePayloadXML(t, result)
	if got != xml {
		t.Errorf("XML with special chars not preserved:\n  got:  %q\n  want: %q", got, xml)
	}
}

func TestPatchDefineXML_HookDirChain(t *testing.T) {
	// Test patchDefineXML with a hook directory chain (two hooks).
	hooksDir := t.TempDir()

	// First hook: wraps in <outer> tag.
	writeScript(t, filepath.Join(hooksDir, "01-wrap.sh"),
		`input=$(cat); echo "<outer>${input}</outer>"`)

	// Second hook: passes through.
	writeScript(t, filepath.Join(hooksDir, "02-noop.sh"), "cat\n")

	hc, err := NewHookChain(testLogger(), "", hooksDir, "")
	if err != nil {
		t.Fatalf("NewHookChain: %v", err)
	}

	xml := "<inner/>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := decodePayloadXML(t, result)
	expected := "<outer><inner/></outer>\n"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestPatchDefineXML_UnalignedXMLLength(t *testing.T) {
	// Ensure XDR padding is handled correctly for various string lengths.
	hooks := makeHookChain(t, "cat\n")

	// Test lengths 1..7 to cover all padding cases (0-3 bytes).
	for i := 1; i <= 7; i++ {
		xml := string(bytes.Repeat([]byte("x"), i))
		payload := makeXDRPayload(xml)

		result, err := patchDefineXML(testLogger(), payload, hooks)
		if err != nil {
			t.Fatalf("len=%d: unexpected error: %v", i, err)
		}
		if !bytes.Equal(result, payload) {
			t.Errorf("len=%d: payload should be unchanged on passthrough", i)
		}
	}
}

func TestPatchDefineXML_HookModifiesWithFlagsMultipleAlignments(t *testing.T) {
	// Verify that flags are preserved for several modified XML sizes that
	// exercise different XDR padding alignments.
	replacements := []string{
		"a",        // 1 byte → 3 pad
		"ab",       // 2 bytes → 2 pad
		"abc",      // 3 bytes → 1 pad
		"abcd",     // 4 bytes → 0 pad
		"abcde",    // 5 bytes → 3 pad
		"abcdefgh", // 8 bytes → 0 pad
	}

	for _, repl := range replacements {
		t.Run("repl_len_"+string(rune('0'+len(repl))), func(t *testing.T) {
			dir := t.TempDir()
			hookFile := filepath.Join(dir, "hook.sh")
			writeScript(t, hookFile, `printf '%s' "`+repl+`"`)
			hc, err := NewHookChain(testLogger(), hookFile, "", "")
			if err != nil {
				t.Fatalf("NewHookChain: %v", err)
			}

			xml := "<domain/>"
			flags := uint32(0x42)
			payload := makeXDRPayloadWithFlags(xml, flags)

			result, err := patchDefineXML(testLogger(), payload, hc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotXML, consumed, err := DecodeXDRString(result)
			if err != nil {
				t.Fatalf("DecodeXDRString: %v", err)
			}
			if gotXML != repl {
				t.Errorf("XML = %q, want %q", gotXML, repl)
			}

			remainder := result[consumed:]
			if len(remainder) != 4 {
				t.Fatalf("trailing data = %d bytes, want 4", len(remainder))
			}
			gotFlags := binary.BigEndian.Uint32(remainder)
			if gotFlags != flags {
				t.Errorf("flags = %#x, want %#x", gotFlags, flags)
			}
		})
	}
}

func TestPatchDefineXML_HookChainError(t *testing.T) {
	// Build a HookChain with a nonexistent hookDirPath to force
	// Run() to return an error from listHookDir.
	hc := &HookChain{
		hookPath:       "",
		hookDirPath:    "/nonexistent/hooks.d",
		hookDomainName: defaultHookDomainName,
		logger:         testLogger().With("component", "hooks"),
	}

	xml := "<domain/>"
	payload := makeXDRPayload(xml)

	result, err := patchDefineXML(testLogger(), payload, hc)
	if err == nil {
		t.Fatal("expected error when hook chain returns error")
	}
	// Original payload must be returned on error.
	if !bytes.Equal(result, payload) {
		t.Errorf("expected original payload on hook chain error")
	}
}

// ---------------------------------------------------------------------------
// patchDefineXML: idempotency
// ---------------------------------------------------------------------------

func TestPatchDefineXML_IdempotentPassthrough(t *testing.T) {
	// Running patchDefineXML twice on the same payload with a passthrough
	// hook should produce the same result both times.
	hooks := makeHookChain(t, "cat\n")

	xml := "<domain><name>idempotent</name></domain>"
	payload := makeXDRPayload(xml)

	result1, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	result2, err := patchDefineXML(testLogger(), result1, hooks)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if !bytes.Equal(result1, result2) {
		t.Error("passthrough is not idempotent")
	}
}

func TestPatchDefineXML_IdempotentModify(t *testing.T) {
	// A hook that always replaces to the same output should be idempotent
	// after the first call.
	hooks := makeHookChain(t, `echo "<domain><name>fixed</name></domain>"`)

	xml := "<domain><name>original</name></domain>"
	payload := makeXDRPayload(xml)

	result1, err := patchDefineXML(testLogger(), payload, hooks)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	result2, err := patchDefineXML(testLogger(), result1, hooks)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	// After the first modification, subsequent calls with the same hook
	// should return the same payload (the hook's output matches the input).
	got1 := decodePayloadXML(t, result1)
	got2 := decodePayloadXML(t, result2)
	if got1 != got2 {
		t.Errorf("not idempotent: first=%q, second=%q", got1, got2)
	}
}

// ---------------------------------------------------------------------------
// patchDefineXML: verify XDR structure integrity
// ---------------------------------------------------------------------------

func TestPatchDefineXML_ResultIsValidXDR(t *testing.T) {
	// Verify that the result payload is always a valid XDR-encoded string,
	// even when the hook dramatically changes the XML size.
	testCases := []struct {
		name   string
		input  string
		script string
	}{
		{"grow", "<d/>", `echo "<domain type='kvm'><name>grown</name><memory>1048576</memory></domain>"`},
		{"shrink", "<domain type='kvm'><name>big</name><memory>1048576</memory></domain>", `echo "<d/>"`},
		{"same-size", "<domain/>", `printf '<replaced/>'`},
		{"whitespace-only-output", "<domain/>", `echo "   "  `}, // whitespace-only = no changes
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hooks := makeHookChain(t, tc.script)
			payload := makeXDRPayload(tc.input)

			result, err := patchDefineXML(testLogger(), payload, hooks)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// The result must be decodable as a valid XDR string.
			_, _, err = DecodeXDRString(result)
			if err != nil {
				t.Fatalf("result is not valid XDR: %v", err)
			}
		})
	}
}
