// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later

package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseHeader
// ---------------------------------------------------------------------------

func TestParseHeader_Valid(t *testing.T) {
	buf := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(buf[0:4], RemoteProgram)
	binary.BigEndian.PutUint32(buf[4:8], RemoteProtocolVersion)
	binary.BigEndian.PutUint32(buf[8:12], ProcDomainDefineXML)
	binary.BigEndian.PutUint32(buf[12:16], TypeCall)
	binary.BigEndian.PutUint32(buf[16:20], 42)
	binary.BigEndian.PutUint32(buf[20:24], 0)

	h, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Program != RemoteProgram {
		t.Errorf("Program = %#x, want %#x", h.Program, RemoteProgram)
	}
	if h.Version != RemoteProtocolVersion {
		t.Errorf("Version = %d, want %d", h.Version, RemoteProtocolVersion)
	}
	if h.Procedure != ProcDomainDefineXML {
		t.Errorf("Procedure = %d, want %d", h.Procedure, ProcDomainDefineXML)
	}
	if h.Type != TypeCall {
		t.Errorf("Type = %d, want %d", h.Type, TypeCall)
	}
	if h.Serial != 42 {
		t.Errorf("Serial = %d, want 42", h.Serial)
	}
	if h.Status != 0 {
		t.Errorf("Status = %d, want 0", h.Status)
	}
}

func TestParseHeader_TooShort(t *testing.T) {
	_, err := ParseHeader(make([]byte, HeaderSize-1))
	if err == nil {
		t.Fatal("expected error for buffer shorter than HeaderSize")
	}
}

func TestParseHeader_LargerBuffer(t *testing.T) {
	// A buffer larger than HeaderSize should work (extra bytes ignored).
	buf := make([]byte, HeaderSize+16)
	binary.BigEndian.PutUint32(buf[0:4], 0x12345678)
	binary.BigEndian.PutUint32(buf[4:8], 99)

	h, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Program != 0x12345678 {
		t.Errorf("Program = %#x, want 0x12345678", h.Program)
	}
	if h.Version != 99 {
		t.Errorf("Version = %d, want 99", h.Version)
	}
}

// ---------------------------------------------------------------------------
// MarshalHeader
// ---------------------------------------------------------------------------

func TestMarshalHeader_Size(t *testing.T) {
	buf := MarshalHeader(Header{})
	if len(buf) != HeaderSize {
		t.Fatalf("MarshalHeader returned %d bytes, want %d", len(buf), HeaderSize)
	}
}

func TestMarshalHeader_RoundTrip(t *testing.T) {
	original := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXMLFlags,
		Type:      TypeCall,
		Serial:    1001,
		Status:    0,
	}

	buf := MarshalHeader(original)
	parsed, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader failed: %v", err)
	}
	if parsed != original {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", parsed, original)
	}
}

func TestMarshalHeader_ByteOrder(t *testing.T) {
	h := Header{
		Program: 0x20008086,
	}
	buf := MarshalHeader(h)
	// First 4 bytes should be big-endian 0x20008086.
	if buf[0] != 0x20 || buf[1] != 0x00 || buf[2] != 0x80 || buf[3] != 0x86 {
		t.Errorf("big-endian encoding wrong: got [%02x %02x %02x %02x]",
			buf[0], buf[1], buf[2], buf[3])
	}
}

// ---------------------------------------------------------------------------
// IsDomainDefineCall
// ---------------------------------------------------------------------------

func TestIsDomainDefineCall_DefineXML(t *testing.T) {
	h := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXML,
		Type:      TypeCall,
	}
	if !IsDomainDefineCall(h) {
		t.Error("expected true for ProcDomainDefineXML call")
	}
}

func TestIsDomainDefineCall_DefineXMLFlags(t *testing.T) {
	h := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXMLFlags,
		Type:      TypeCall,
	}
	if !IsDomainDefineCall(h) {
		t.Error("expected true for ProcDomainDefineXMLFlags call")
	}
}

func TestIsDomainDefineCall_WrongProgram(t *testing.T) {
	h := Header{
		Program:   0xDEADBEEF,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXML,
		Type:      TypeCall,
	}
	if IsDomainDefineCall(h) {
		t.Error("expected false for wrong Program")
	}
}

func TestIsDomainDefineCall_WrongVersion(t *testing.T) {
	h := Header{
		Program:   RemoteProgram,
		Version:   999,
		Procedure: ProcDomainDefineXML,
		Type:      TypeCall,
	}
	if IsDomainDefineCall(h) {
		t.Error("expected false for wrong Version")
	}
}

func TestIsDomainDefineCall_WrongType(t *testing.T) {
	h := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXML,
		Type:      TypeReply,
	}
	if IsDomainDefineCall(h) {
		t.Error("expected false for TypeReply")
	}
}

func TestIsDomainDefineCall_WrongProcedure(t *testing.T) {
	h := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: 99,
		Type:      TypeCall,
	}
	if IsDomainDefineCall(h) {
		t.Error("expected false for unrecognized procedure")
	}
}

// ---------------------------------------------------------------------------
// ReadMessage / WriteMessage round-trip
// ---------------------------------------------------------------------------

// buildRawMessage builds a complete wire-format message (length prefix +
// header + payload) for feeding into a connection.
func buildRawMessage(h Header, payload []byte) []byte {
	totalLen := uint32(LengthPrefixSize + HeaderSize + len(payload))
	buf := make([]byte, totalLen)
	binary.BigEndian.PutUint32(buf[0:4], totalLen)
	copy(buf[LengthPrefixSize:LengthPrefixSize+HeaderSize], MarshalHeader(h))
	copy(buf[LengthPrefixSize+HeaderSize:], payload)
	return buf
}

func TestReadMessage_Valid(t *testing.T) {
	expectedHeader := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXML,
		Type:      TypeCall,
		Serial:    7,
		Status:    0,
	}
	expectedPayload := []byte("test-payload-data")

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Write the raw message in a goroutine so ReadMessage can consume it.
	go func() {
		raw := buildRawMessage(expectedHeader, expectedPayload)
		client.Write(raw)
		client.Close()
	}()

	header, payload, err := ReadMessage(server)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if header != expectedHeader {
		t.Errorf("header mismatch:\n  got:  %+v\n  want: %+v", header, expectedHeader)
	}
	if !bytes.Equal(payload, expectedPayload) {
		t.Errorf("payload mismatch:\n  got:  %q\n  want: %q", payload, expectedPayload)
	}
}

func TestReadMessage_EmptyPayload(t *testing.T) {
	expectedHeader := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: 1,
		Type:      TypeReply,
		Serial:    1,
		Status:    0,
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		raw := buildRawMessage(expectedHeader, nil)
		client.Write(raw)
		client.Close()
	}()

	header, payload, err := ReadMessage(server)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if header != expectedHeader {
		t.Errorf("header mismatch:\n  got:  %+v\n  want: %+v", header, expectedHeader)
	}
	if len(payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(payload))
	}
}

func TestReadMessage_EOF(t *testing.T) {
	// Connection closed immediately → error reading length prefix.
	client, server := net.Pipe()
	client.Close()

	_, _, err := ReadMessage(server)
	server.Close()
	if err == nil {
		t.Fatal("expected error on EOF")
	}
}

func TestReadMessage_TooShortLength(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		// Write a length value that's smaller than LengthPrefixSize + HeaderSize.
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], 10) // 10 < 28
		client.Write(buf[:])
		client.Close()
	}()

	_, _, err := ReadMessage(server)
	if err == nil {
		t.Fatal("expected error for message too short")
	}
}

func TestReadMessage_TooLargeLength(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		// dataLength = totalLength - 4 > MaxMessageSize.
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], MaxMessageSize+LengthPrefixSize+1)
		client.Write(buf[:])
		client.Close()
	}()

	_, _, err := ReadMessage(server)
	if err == nil {
		t.Fatal("expected error for message too large")
	}
}

func TestReadMessage_TruncatedData(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		// Claim a valid length but only write part of the data.
		var buf [4]byte
		totalLen := uint32(LengthPrefixSize + HeaderSize + 100) // claims 100 bytes of payload
		binary.BigEndian.PutUint32(buf[:], totalLen)
		client.Write(buf[:])
		// Only write 10 bytes of data (less than HeaderSize + 100).
		client.Write(make([]byte, 10))
		client.Close()
	}()

	_, _, err := ReadMessage(server)
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestWriteMessage_Valid(t *testing.T) {
	header := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXMLFlags,
		Type:      TypeCall,
		Serial:    55,
		Status:    0,
	}
	payload := []byte("hello-world")

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Write in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- WriteMessage(server, header, payload)
	}()

	// Read back the raw bytes and verify.
	expectedLen := LengthPrefixSize + HeaderSize + len(payload)
	buf := make([]byte, expectedLen)
	n := 0
	for n < expectedLen {
		nn, err := client.Read(buf[n:])
		if err != nil {
			t.Fatalf("read error after %d bytes: %v", n, err)
		}
		n += nn
	}

	if err := <-errCh; err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}

	// Check the length prefix.
	gotLen := binary.BigEndian.Uint32(buf[0:4])
	if gotLen != uint32(expectedLen) {
		t.Errorf("length prefix = %d, want %d", gotLen, expectedLen)
	}

	// Check the header.
	gotHeader, err := ParseHeader(buf[LengthPrefixSize : LengthPrefixSize+HeaderSize])
	if err != nil {
		t.Fatalf("ParseHeader failed: %v", err)
	}
	if gotHeader != header {
		t.Errorf("header mismatch:\n  got:  %+v\n  want: %+v", gotHeader, header)
	}

	// Check the payload.
	gotPayload := buf[LengthPrefixSize+HeaderSize:]
	if !bytes.Equal(gotPayload, payload) {
		t.Errorf("payload mismatch: got %q, want %q", gotPayload, payload)
	}
}

func TestWriteMessage_ClosedConn(t *testing.T) {
	client, server := net.Pipe()
	client.Close()
	server.Close()

	err := WriteMessage(server, Header{}, []byte("data"))
	if err == nil {
		t.Fatal("expected error writing to closed connection")
	}
}

// ---------------------------------------------------------------------------
// ReadMessage + WriteMessage full round-trip
// ---------------------------------------------------------------------------

func TestReadWriteMessage_RoundTrip(t *testing.T) {
	header := Header{
		Program:   RemoteProgram,
		Version:   RemoteProtocolVersion,
		Procedure: ProcDomainDefineXML,
		Type:      TypeCall,
		Serial:    123,
		Status:    0,
	}
	payload := []byte("<domain type='kvm'><name>test</name></domain>")

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Writer side.
	errCh := make(chan error, 1)
	go func() {
		errCh <- WriteMessage(server, header, payload)
	}()

	// Reader side.
	gotHeader, gotPayload, err := ReadMessage(client)
	if err != nil {
		t.Fatalf("ReadMessage error: %v", err)
	}
	if writeErr := <-errCh; writeErr != nil {
		t.Fatalf("WriteMessage error: %v", writeErr)
	}

	if gotHeader != header {
		t.Errorf("header mismatch:\n  got:  %+v\n  want: %+v", gotHeader, header)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Errorf("payload mismatch:\n  got:  %q\n  want: %q", gotPayload, payload)
	}
}

func TestReadWriteMessage_RoundTripMultiple(t *testing.T) {
	// Send multiple messages over the same connection.
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	messages := []struct {
		header  Header
		payload []byte
	}{
		{
			Header{RemoteProgram, RemoteProtocolVersion, ProcDomainDefineXML, TypeCall, 1, 0},
			[]byte("first"),
		},
		{
			Header{RemoteProgram, RemoteProtocolVersion, 99, TypeReply, 2, 0},
			nil,
		},
		{
			Header{RemoteProgram, RemoteProtocolVersion, ProcDomainDefineXMLFlags, TypeCall, 3, 0},
			[]byte("third-message-with-longer-payload"),
		},
	}

	errCh := make(chan error, 1)
	go func() {
		for _, m := range messages {
			if err := WriteMessage(server, m.header, m.payload); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	for i, want := range messages {
		gotHeader, gotPayload, err := ReadMessage(client)
		if err != nil {
			t.Fatalf("message %d: ReadMessage error: %v", i, err)
		}
		if gotHeader != want.header {
			t.Errorf("message %d: header mismatch:\n  got:  %+v\n  want: %+v", i, gotHeader, want.header)
		}
		if !bytes.Equal(gotPayload, want.payload) {
			t.Errorf("message %d: payload mismatch:\n  got:  %q\n  want: %q", i, gotPayload, want.payload)
		}
	}

	if err := <-errCh; err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}
}
