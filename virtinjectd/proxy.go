// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
)

type Proxy struct {
	Logger       *slog.Logger
	UpstreamPath string
	Hooks        *HookChain
}

// Run accepts client connections and spawns a goroutine to handle
// each one. It returns when the listener is closed (e.g., on shutdown).
func (proxy *Proxy) Run(listener net.Listener) {
	for {
		clientConn, err := listener.Accept()
		if err != nil {
			// Check if this is due to the listener being closed (shutdown).
			if isConnectionClosed(err) {
				proxy.Logger.Info("listener closed, stopping accept loop")
				return
			}
			proxy.Logger.Error("failed to accept connection", "err", err)
			continue
		}

		// Handle each client connection in its own goroutine.
		go proxy.handleConnection(clientConn)
	}
}

// handleConnection manages a single proxied connection. It creates a
// paired connection to the upstream libvirtd socket and bidirectionally
// forwards all traffic between the client and upstream.
//
// DomainDefineXML and DomainDefineXMLFlags calls (procedures 11 and 350)
// are intercepted on the client-to-server path: the domain XML is
// extracted from the XDR payload, passed through the hook chain, and the
// (possibly modified) XML is re-encoded before forwarding to upstream.
//
// All other messages are forwarded as raw bytes without inspection.
func (proxy *Proxy) handleConnection(clientConn net.Conn) {
	connLogger := proxy.Logger.With("component", "conn", "client", clientConn.RemoteAddr())
	connLogger.Info("new client connection")

	defer func() {
		clientConn.Close()
		connLogger.Info("client connection closed")
	}()

	// Establish the paired upstream connection to the real libvirtd.
	upstreamConn, err := net.Dial("unix", proxy.UpstreamPath)
	if err != nil {
		connLogger.Error("failed to connect to upstream", "err", err, "path", proxy.UpstreamPath)
		return
	}
	defer upstreamConn.Close()

	connLogger.Debug("connected to upstream", "path", proxy.UpstreamPath)

	// Use a WaitGroup to wait for both forwarding goroutines to finish.
	// When either direction encounters an error (including EOF), we close
	// both connections to unblock the other goroutine.
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Server
	intercept := func(header Header, payload []byte) []byte {
		// Check if this is a DomainDefineXML or DomainDefineXMLFlags call
		// that we need to intercept.
		if !IsDomainDefineCall(header) {
			return payload
		}
		proxy.Logger.Info("intercepted DomainDefineXML call",
			"procedure", header.Procedure,
			"serial", header.Serial,
		)
		updatedPayload, err := patchDefineXML(proxy.Logger, payload, proxy.Hooks)
		if err != nil {
			// Log the error but forward the original payload.
			// This is the safe choice: we don't want to block VM
			// creation just because the hook failed.
			proxy.Logger.Error("hook chain failed, forwarding original XML", "err", err)
			return payload
		}
		return updatedPayload
	}
	go func() {
		defer wg.Done()
		if err := forward(connLogger, clientConn, upstreamConn, intercept); err != nil {
			// Only log if it's not a normal connection close.
			if !isConnectionClosed(err) {
				connLogger.Error("client-to-server forwarding error", "err", err)
			}
		}
		// Close both sides to unblock the other goroutine.
		clientConn.Close()
		upstreamConn.Close()
	}()

	// Server -> Client
	passthrough := func(_ Header, payload []byte) []byte {
		return payload
	}
	go func() {
		defer wg.Done()
		if err := forward(connLogger, upstreamConn, clientConn, passthrough); err != nil {
			if !isConnectionClosed(err) {
				connLogger.Error("server-to-client forwarding error", "err", err)
			}
		}
		// Close both sides to unblock the other goroutine.
		clientConn.Close()
		upstreamConn.Close()
	}()

	wg.Wait()
}

func forward(logger *slog.Logger, src, dst net.Conn, processor func(Header, []byte) []byte) error {
	for {
		header, payload, err := ReadMessage(src)
		if err != nil {
			return err
		}
		payload = processor(header, payload)
		err = WriteMessage(dst, header, payload)
		if err != nil {
			return err
		}
	}
}

// patchDefineXML extracts the domain XML from the XDR payload, runs it
// through the hook chain, and returns the payload with the (possibly
// modified) XML re-encoded.
//
// The payload structure for DomainDefineXML (proc 11) is:
//
//	{ remote_nonnull_string xml }
//
// For DomainDefineXMLFlags (proc 350):
//
//	{ remote_nonnull_string xml, unsigned int flags }
//
// In both cases, the XML is the first XDR string in the payload.
// ReplaceXDRString handles preserving any data after the string (the
// flags field for proc 350).
func patchDefineXML(logger *slog.Logger, payload []byte, hooks *HookChain) ([]byte, error) {
	// Step 1: Decode the XML string from the payload.
	xmlStr, _, err := DecodeXDRString(payload)
	if err != nil {
		return payload, err
	}

	logger.Debug("extracted domain XML from payload", "xmlLen", len(xmlStr))
	logger.Log(context.Background(), LevelTraceXML, "original domain XML", "xml", xmlStr)

	// Step 2: Run the hook chain to (possibly) modify the XML.
	patchedXML, err := hooks.Run(xmlStr)
	if err != nil {
		return payload, err
	}

	// Step 3: If the XML was not modified, return the original payload.
	if patchedXML == xmlStr {
		logger.Debug("hooks did not modify the XML")
		return payload, nil
	}

	logger.Info("hooks modified the domain XML",
		"originalLen", len(xmlStr),
		"patchedLen", len(patchedXML),
	)
	logger.Log(context.Background(), LevelTraceXML, "patched domain XML", "xml", patchedXML)

	// Step 4: Re-encode the patched XML into the payload.
	newPayload, err := ReplaceXDRString(payload, patchedXML)
	if err != nil {
		return payload, err
	}

	return newPayload, nil
}

// isConnectionClosed checks if an error indicates a normal connection
// closure (EOF or "use of closed network connection").
func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	// net.ErrClosed is returned when reading/writing a closed connection.
	if err == net.ErrClosed {
		return true
	}
	// Check the error string for the common "use of closed" message,
	// which is returned by the net package in various forms.
	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset by peer")
}
