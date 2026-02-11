// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later
//
// Protocol constants and structure definitions in this file are derived from
// the libvirt source code (LGPL-2.1-or-later, Copyright Red Hat, Inc.):
//   - src/remote/remote_protocol.x
//   - src/rpc/virnetprotocol.x
// No C code has been copied or translated; only numeric constants and
// structural knowledge are used. See README.md for full provenance details.

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// -----------------------------------------------------------------------
// Protocol constants derived from libvirt source files.
//
// From src/remote/remote_protocol.x (LGPL-2.1+, Copyright Red Hat, Inc.):
//   REMOTE_PROGRAM = 0x20008086
//   REMOTE_PROTOCOL_VERSION = 1
//   REMOTE_PROC_DOMAIN_DEFINE_XML = 11
//   REMOTE_PROC_DOMAIN_DEFINE_XML_FLAGS = 350
//
// From src/rpc/virnetprotocol.x (LGPL-2.1+, Copyright Red Hat, Inc.):
//   VIR_NET_MESSAGE_HEADER_MAX = 24
//   VIR_NET_MESSAGE_LEN_MAX = 4
//   VIR_NET_MESSAGE_MAX = 33554432
//   virNetMessageType enum values
// -----------------------------------------------------------------------

const (
	// RemoteProgram is the unique program ID for the libvirt remote driver.
	RemoteProgram uint32 = 0x20008086

	// RemoteProtocolVersion is the protocol version number.
	RemoteProtocolVersion uint32 = 1

	// ProcDomainDefineXML is the procedure number for virDomainDefineXML.
	// Args: { remote_nonnull_string xml }
	ProcDomainDefineXML uint32 = 11

	// ProcDomainDefineXMLFlags is the procedure number for virDomainDefineXMLFlags.
	// Args: { remote_nonnull_string xml, unsigned int flags }
	ProcDomainDefineXMLFlags uint32 = 350
)

// Message type constants from virNetMessageType enum.
const (
	// TypeCall is a client-to-server method invocation.
	TypeCall uint32 = 0

	// TypeReply is a server-to-client reply to a method call.
	TypeReply uint32 = 1

	// TypeMessage is an asynchronous event notification (either direction).
	TypeMessage uint32 = 2

	// TypeStream is a stream data packet (either direction).
	TypeStream uint32 = 3

	// TypeCallWithFDs is a client-to-server call that also passes file descriptors.
	TypeCallWithFDs uint32 = 4

	// TypeReplyWithFDs is a server-to-client reply that also passes file descriptors.
	TypeReplyWithFDs uint32 = 5
)

// Size constants for the wire protocol.
const (
	// HeaderSize is the size of the serialized virNetMessageHeader (6 x uint32).
	HeaderSize = 24

	// LengthPrefixSize is the size of the message length field (uint32).
	LengthPrefixSize = 4

	// MaxMessageSize is the maximum total message size (excluding length prefix).
	// From VIR_NET_MESSAGE_MAX = 33554432 (32 MiB).
	MaxMessageSize = 33554432
)

// Header represents the virNetMessageHeader structure from the libvirt
// RPC wire protocol. All fields are big-endian uint32 on the wire.
//
// Wire layout (24 bytes total):
//
//	+---------------+
//	| Program  U32  |  Unique ID for the program (e.g., 0x20008086)
//	+---------------+
//	| Version  U32  |  Protocol version (e.g., 1)
//	+---------------+
//	| Procedure S32 |  Procedure number within the program
//	+---------------+
//	| Type     S32  |  Message type (call, reply, stream, etc.)
//	+---------------+
//	| Serial   U32  |  Request serial number
//	+---------------+
//	| Status   S32  |  Status (ok, error, continue)
//	+---------------+
type Header struct {
	Program   uint32
	Version   uint32
	Procedure uint32
	Type      uint32
	Serial    uint32
	Status    uint32
}

// ParseHeader decodes a 24-byte big-endian buffer into a Header.
// The buffer must be exactly HeaderSize (24) bytes.
func ParseHeader(buf []byte) (Header, error) {
	if len(buf) < HeaderSize {
		return Header{}, fmt.Errorf("header buffer too short: %d < %d", len(buf), HeaderSize)
	}
	return Header{
		Program:   binary.BigEndian.Uint32(buf[0:4]),
		Version:   binary.BigEndian.Uint32(buf[4:8]),
		Procedure: binary.BigEndian.Uint32(buf[8:12]),
		Type:      binary.BigEndian.Uint32(buf[12:16]),
		Serial:    binary.BigEndian.Uint32(buf[16:20]),
		Status:    binary.BigEndian.Uint32(buf[20:24]),
	}, nil
}

// MarshalHeader encodes a Header into a 24-byte big-endian buffer.
func MarshalHeader(h Header) []byte {
	buf := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(buf[0:4], h.Program)
	binary.BigEndian.PutUint32(buf[4:8], h.Version)
	binary.BigEndian.PutUint32(buf[8:12], h.Procedure)
	binary.BigEndian.PutUint32(buf[12:16], h.Type)
	binary.BigEndian.PutUint32(buf[16:20], h.Serial)
	binary.BigEndian.PutUint32(buf[20:24], h.Status)
	return buf
}

// ReadMessage reads a complete libvirt RPC message from a connection.
//
// Wire format:
//
//	[4-byte length (big-endian, includes itself)] [24-byte header] [payload]
//
// Returns the parsed header and the raw payload bytes (everything after the header).
// The length prefix is consumed but not returned.
func ReadMessage(conn net.Conn) (Header, []byte, error) {
	// Step 1: Read the 4-byte length prefix.
	var lengthBuf [LengthPrefixSize]byte
	if _, err := io.ReadFull(conn, lengthBuf[:]); err != nil {
		return Header{}, nil, fmt.Errorf("reading message length: %w", err)
	}
	totalLength := binary.BigEndian.Uint32(lengthBuf[:])

	// Sanity check the length. The length includes the 4-byte length word itself.
	if totalLength < LengthPrefixSize+HeaderSize {
		return Header{}, nil, fmt.Errorf("message too short: %d bytes", totalLength)
	}
	dataLength := totalLength - LengthPrefixSize
	if dataLength > MaxMessageSize {
		return Header{}, nil, fmt.Errorf("message too large: %d > %d", dataLength, MaxMessageSize)
	}

	// Step 2: Read the rest of the message (header + payload).
	data := make([]byte, dataLength)
	if _, err := io.ReadFull(conn, data); err != nil {
		return Header{}, nil, fmt.Errorf("reading message data (%d bytes): %w", dataLength, err)
	}

	// Step 3: Parse the header from the first 24 bytes.
	header, err := ParseHeader(data[:HeaderSize])
	if err != nil {
		return Header{}, nil, fmt.Errorf("parsing header: %w", err)
	}

	// Step 4: The payload is everything after the header.
	payload := data[HeaderSize:]

	return header, payload, nil
}

// WriteMessage writes a complete libvirt RPC message to a connection.
//
// It prepends the 4-byte length prefix (which includes itself), then writes
// the 24-byte header and the payload.
func WriteMessage(conn net.Conn, header Header, payload []byte) error {
	// Total length = length prefix + header + payload.
	totalLength := uint32(LengthPrefixSize + HeaderSize + len(payload))

	// Build the complete message buffer to write atomically.
	buf := make([]byte, totalLength)
	binary.BigEndian.PutUint32(buf[0:4], totalLength)
	copy(buf[LengthPrefixSize:LengthPrefixSize+HeaderSize], MarshalHeader(header))
	copy(buf[LengthPrefixSize+HeaderSize:], payload)

	// Write the entire message in one call to avoid interleaving with
	// concurrent writes on the same connection.
	if _, err := conn.Write(buf); err != nil {
		return fmt.Errorf("writing message (%d bytes): %w", totalLength, err)
	}
	return nil
}

// IsDomainDefineCall returns true if the header represents a
// virDomainDefineXML or virDomainDefineXMLFlags RPC call from the
// libvirt remote program.
func IsDomainDefineCall(h Header) bool {
	return h.Program == RemoteProgram &&
		h.Version == RemoteProtocolVersion &&
		h.Type == TypeCall &&
		(h.Procedure == ProcDomainDefineXML || h.Procedure == ProcDomainDefineXMLFlags)
}
