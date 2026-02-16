// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later

// virtinjectd is a transparent libvirt RPC proxy that intercepts
// virDomainDefineXML calls and passes the domain XML through a chain of
// hook scripts before forwarding to the real libvirtd daemon.
//
// virtinjectd is meant to be a developer/exploration tool
// **AND SHOULD NEVER EVER BE USED IN PRODUCTION ENVIRONMENTS**
//
// virtinjectd workarounds a design decision of libvirt where the qemu hook
// scripts' output is discarded during the "prepare" and "start" phases,
// making it impossible to modify domain XML at define time via hooks alone.
//
// In case we can't or won't change the client application, using
// a solution like virtinjectd becomes the only feasible option.
//
// The proxy listens on a Unix socket and forwards all traffic to the real
// libvirtd socket. Only DomainDefineXML and DomainDefineXMLFlags RPC calls
// are intercepted; all other traffic passes through unchanged.
//
// Usage:
//
//	virtinjectd \
//	  --listen /tmp/libvirt-proxy.sock \
//	  --upstream /var/run/libvirt/libvirt-sock \
//	  --hook /path/to/hook-script.py
//
// Then point minikube (or any libvirt client) at the proxy socket:
//
//	minikube start --driver=kvm2 \
//	  --kvm-qemu-uri='qemu+unix:///system?socket=/tmp/libvirt-proxy.sock'
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// Custom slog levels for fine-grained verbosity, extending the standard
// Debug level downward. This preserves the original V(0)..V(3) semantics:
//
//	V(0) = Info   → slog.LevelInfo  (0)
//	V(1) = Debug  → slog.LevelDebug (-4)
//	V(2) = Trace  → LevelTrace      (-8)
//	V(3) = Trace+XML → LevelTraceXML (-12)
const (
	LevelTrace    = slog.Level(-8)
	LevelTraceXML = slog.Level(-12)
)

func main() {
	listenPath := flag.String("listen", "/tmp/libvirt-proxy.sock",
		"Path for the proxy Unix socket that clients connect to.")

	upstreamPath := flag.String("upstream", "/var/run/libvirt/libvirt-sock",
		"Path to the real libvirtd Unix socket to forward traffic to.")

	hookPath := flag.String("hook", "",
		"Path to a single hook script. The script is invoked with the\n"+
			"libvirt hook calling convention:\n"+
			"  argv: [script, <hook-domain-name>, \"prepare\", \"begin\", \"-\"]\n"+
			"  stdin: domain XML\n"+
			"  stdout: modified domain XML (empty = no changes)")

	hookDirPath := flag.String("hook-dir", "",
		"Path to a directory of hook scripts (like /etc/libvirt/hooks/qemu.d/).\n"+
			"Scripts are executed in alphabetical order, each acting as a filter:\n"+
			"stdout of one becomes stdin of the next.")

	hookDomainName := flag.String("hook-domain-name", "virtinjectd",
		"Domain name passed as argv[1] to hook scripts. Since the proxy\n"+
			"intercepts at the RPC level before the domain is defined, the\n"+
			"real domain name is not available. Hooks that need it should\n"+
			"extract it from the XML on stdin.")

	verbosity := flag.Int("v", 0,
		"Logging verbosity level. 0=info, 1=debug, 2=trace, 3=trace with XML.")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.Level(-4 * *verbosity),
	}))
	logger = logger.With("component", "virtinjectd")

	if *hookPath == "" && *hookDirPath == "" {
		fmt.Fprintf(os.Stderr, "Error: at least one of --hook or --hook-dir must be specified.\n\n")
		flag.Usage()
		os.Exit(1)
	}

	hooks, err := NewHookChain(logger, *hookPath, *hookDirPath, *hookDomainName)
	if err != nil {
		logger.Error("failed to initialize hook chain", "err", err)
		os.Exit(1)
	}

	logger.Info("hook chain initialized", "hook", *hookPath, "hookDir", *hookDirPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Remove any stale socket file from a previous run.
	if err := os.Remove(*listenPath); err != nil && !os.IsNotExist(err) {
		logger.Error("failed to remove stale socket file", "err", err, "path", *listenPath)
		os.Exit(1)
	}

	listener, err := net.Listen("unix", *listenPath)
	if err != nil {
		logger.Error("failed to listen on Unix socket", "err", err, "path", *listenPath)
		os.Exit(1)
	}

	logger.Info("listening for connections", "listen", *listenPath, "upstream", *upstreamPath)
	fmt.Printf("run \"export LIBVIRT_PROXY_SOCK=%s\"", *listenPath)

	defer func() {
		listener.Close()
		os.Remove(*listenPath)
		logger.Info("shutdown complete")
	}()

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		listener.Close()
	}()

	proxy := Proxy{
		Logger:       logger,
		UpstreamPath: *upstreamPath,
		Hooks:        hooks,
	}
	proxy.Run(listener)
}
