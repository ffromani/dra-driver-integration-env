// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later
//
// XDR encoding follows RFC 4506 (public standard).
// The maximum string length constant (4194304) is derived from
// REMOTE_STRING_MAX in libvirt's src/remote/remote_protocol.x
// (LGPL-2.1-or-later, Copyright Red Hat, Inc.).

package main

import (
	"encoding/binary"
	"fmt"
)

// xdrStringMaxLen is the maximum length of an XDR string in the libvirt
// remote protocol. Derived from REMOTE_STRING_MAX = 4194304 in
// src/remote/remote_protocol.x.
const xdrStringMaxLen = 4194304

// xdrPad returns the number of padding bytes needed to align n to a
// 4-byte boundary, per RFC 4506 section 3.
func xdrPad(n int) int {
	remainder := n % 4
	if remainder == 0 {
		return 0
	}
	return 4 - remainder
}

// DecodeXDRString decodes an XDR variable-length string from the
// beginning of data.
//
// XDR string encoding (RFC 4506, section 4.11):
//
//	+--------+--------...--------+--------...--------+
//	| length |    string data    |      padding      |
//	+--------+--------...--------+--------...--------+
//	 4 bytes    length bytes      0-3 bytes (to align)
//
// Returns the decoded string, the total number of bytes consumed
// (including the 4-byte length and padding), and any error.
func DecodeXDRString(data []byte) (string, int, error) {
	if len(data) < 4 {
		return "", 0, fmt.Errorf("XDR string: need at least 4 bytes for length, got %d", len(data))
	}

	// Read the 4-byte big-endian string length.
	strLen := int(binary.BigEndian.Uint32(data[0:4]))
	if strLen < 0 || strLen > xdrStringMaxLen {
		return "", 0, fmt.Errorf("XDR string: length %d out of range [0, %d]", strLen, xdrStringMaxLen)
	}

	// Calculate total consumed bytes: 4 (length) + strLen + padding.
	pad := xdrPad(strLen)
	totalConsumed := 4 + strLen + pad

	if len(data) < totalConsumed {
		return "", 0, fmt.Errorf("XDR string: need %d bytes, got %d", totalConsumed, len(data))
	}

	return string(data[4 : 4+strLen]), totalConsumed, nil
}

// EncodeXDRString encodes a Go string into XDR variable-length string
// format: 4-byte big-endian length + string bytes + zero padding to
// 4-byte boundary.
func EncodeXDRString(s string) []byte {
	strLen := len(s)
	pad := xdrPad(strLen)
	buf := make([]byte, 4+strLen+pad)
	binary.BigEndian.PutUint32(buf[0:4], uint32(strLen))
	copy(buf[4:], s)
	// Padding bytes are already zero from make().
	return buf
}

// ReplaceXDRString replaces the first XDR string in payload with newStr,
// preserving any data that follows (e.g., the flags field in
// remote_domain_define_xml_flags_args).
//
// Returns the new payload with the replaced string. If decoding fails,
// returns the original payload unchanged and an error.
func ReplaceXDRString(payload []byte, newStr string) ([]byte, error) {
	// Decode the existing string to find how many bytes it occupies.
	_, consumed, err := DecodeXDRString(payload)
	if err != nil {
		return payload, fmt.Errorf("decoding existing XDR string: %w", err)
	}

	// Encode the replacement string.
	newEncoded := EncodeXDRString(newStr)

	// Build the new payload: new encoded string + everything after the old string.
	remainder := payload[consumed:]
	result := make([]byte, len(newEncoded)+len(remainder))
	copy(result, newEncoded)
	copy(result[len(newEncoded):], remainder)

	return result, nil
}
