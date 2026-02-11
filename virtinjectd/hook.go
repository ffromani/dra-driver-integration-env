// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later

// The hook calling convention is identical to libvirt's /etc/libvirt/hooks/qemu:
//   argv[1] = domain name (constant, default "virtinjectd")
//   argv[2] = operation  (always "prepare")
//   argv[3] = sub-operation (always "begin")
//   argv[4] = extra (always "-")
//   stdin   = full domain XML
//   stdout  = modified domain XML (empty = no changes)
// See https://libvirt.org/hooks.html for the full specification.

package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hookTimeout is the maximum time a single hook script is allowed to run
// before being killed. This prevents broken or hanging hooks from blocking
// the virtinjectd process indefinitely.
const hookTimeout = 3 * time.Second

// defaultHookDomainName is the default domain name passed as argv[1] to hook
// scripts. virtinjectd intercepts at the RPC level before the domain is defined,
// so the real domain name is only available inside the XML payload. Existing
// hooks that need the domain name should extract it from the XML on stdin.
const defaultHookDomainName = "virtinjectd"

// HookChain manages the execution of one or more hook scripts in sequence.
// It supports both a single hook script (--hook) and a directory of hook
// scripts (--hook-dir), matching the libvirt qemu + qemu.d/ pattern.
//
// Execution order:
//  1. Single hook script (if configured)
//  2. Directory hook scripts in alphabetical order (if configured)
//
// Each hook acts as a filter: it receives XML on stdin and writes
// (optionally modified) XML to stdout. The output of each hook becomes
// the input to the next hook (chained pipeline).
type HookChain struct {
	// hookPath is the path to a single hook script (may be empty).
	hookPath string

	// hookDirPath is the path to a directory of hook scripts (may be empty).
	hookDirPath string

	// hookDomainName is the domain name passed as argv[1] to hook scripts.
	hookDomainName string

	// logger is used for structured logging throughout hook execution.
	logger *slog.Logger
}

// NewHookChain creates a new HookChain. At least one of hookPath or
// hookDirPath must be non-empty. Both paths are validated for existence
// and appropriate type (file vs directory).
func NewHookChain(logger *slog.Logger, hookPath, hookDirPath, hookDomainName string) (*HookChain, error) {
	if hookPath == "" && hookDirPath == "" {
		return nil, fmt.Errorf("at least one of hookPath or hookDirPath must be specified")
	}

	if hookPath != "" {
		info, err := os.Stat(hookPath)
		if err != nil {
			return nil, fmt.Errorf("hook script %q: %w", hookPath, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("hook script %q is a directory, not a file", hookPath)
		}
		// Check that the file is executable by the current user.
		if info.Mode()&0111 == 0 {
			return nil, fmt.Errorf("hook script %q is not executable", hookPath)
		}
	}

	if hookDirPath != "" {
		info, err := os.Stat(hookDirPath)
		if err != nil {
			return nil, fmt.Errorf("hook directory %q: %w", hookDirPath, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("hook directory %q is not a directory", hookDirPath)
		}
	}

	if hookDomainName == "" {
		hookDomainName = defaultHookDomainName
	}

	return &HookChain{
		hookPath:       hookPath,
		hookDirPath:    hookDirPath,
		hookDomainName: hookDomainName,
		logger:         logger.With("component", "hooks"),
	}, nil
}

// Run executes the full hook chain on the given XML input. It returns the
// (possibly modified) XML output. If no hooks modify the XML, the original
// input is returned unchanged.
//
// The hook chain runs:
//  1. The single hook script (if configured)
//  2. Each script in the hook directory, in alphabetical order (if configured)
//
// Each hook's stdout becomes the next hook's stdin. Empty stdout from any
// hook means "no changes" and the current XML is passed through to the next.
//
// If any hook fails (non-zero exit), the error is logged but execution
// continues with the remaining hooks. The XML that was passed to the
// failing hook is preserved (not modified by the failed hook).
func (hc *HookChain) Run(xmlInput string) (string, error) {
	currentXML := xmlInput
	hooks := []string{}

	// Step 1: Add the single hook script if configured.
	if hc.hookPath != "" {
		hc.logger.Debug("adding single hook", "path", hc.hookPath)
		hooks = append(hooks, hc.hookPath)
	}

	// Step 2: Add directory hooks in alphabetical order if configured.
	if hc.hookDirPath != "" {
		hooksFromDir, err := listHookDir(hc.hookDirPath)
		if err != nil {
			return currentXML, fmt.Errorf("listing hook directory %q: %w", hc.hookDirPath, err)
		}

		for _, hookPath := range hooksFromDir {
			hc.logger.Debug("adding hook from directory", "path", hookPath, "dir", hc.hookDirPath)
			hooks = append(hooks, hookPath)
		}
	}

	// Step 3: Run all the hooks we discovered.
	// We constructed the list in the correct priority order, so we can just run it now.
	hc.logger.Debug("discovered hook", "hookCount", len(hooks))
	for _, hookPath := range hooks {
		hc.logger.Log(context.Background(), LevelTrace, "running hook", "path", hookPath)
		output, err := runHook(hc.logger, hookPath, hc.hookDomainName, currentXML)
		if err != nil {
			hc.logger.Error("hook failed, keeping current XML", "err", err, "path", hookPath)
			continue
		}
		currentXML = output
	}

	return currentXML, nil
}

// runHook executes a single hook script with the libvirt-compatible
// calling convention:
//
//	argv: [hookPath, domainName, "prepare", "begin", "-"]
//	stdin: xmlInput
//	stdout: modified XML (or empty for no changes)
//
// Returns the hook's output XML. If the hook produces empty output, the
// original xmlInput is returned unchanged. If the hook fails (non-zero
// exit), an error is returned.
func runHook(logger *slog.Logger, hookPath string, domainName string, xmlInput string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()

	// Build the command with the libvirt-compatible argv convention.
	// See https://libvirt.org/hooks.html for the full specification.
	//
	//   argv[0] = hook script path
	//   argv[1] = domain name (configurable)
	//   argv[2] = operation ("prepare")
	//   argv[3] = sub-operation ("begin")
	//   argv[4] = extra ("-")
	cmd := exec.CommandContext(ctx, hookPath, domainName, "prepare", "begin", "-")

	// Pipe the domain XML to the hook's stdin.
	cmd.Stdin = strings.NewReader(xmlInput)

	// Capture stdout (modified XML) and stderr (diagnostics).
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Log(ctx, LevelTrace, "executing hook",
		"path", hookPath,
		"argv", []string{hookPath, domainName, "prepare", "begin", "-"},
		"inputLen", len(xmlInput),
	)

	if err := cmd.Run(); err != nil {
		// Log stderr from the hook for debugging.
		if stderr.Len() > 0 {
			logger.Error("hook stderr output",
				"err", err,
				"path", hookPath,
				"stderr", strings.TrimSpace(stderr.String()),
			)
		}
		return xmlInput, fmt.Errorf("hook %q failed: %w", hookPath, err)
	}

	// Log any stderr even on success (hooks may emit warnings).
	if stderr.Len() > 0 {
		logger.Debug("hook stderr output",
			"path", hookPath,
			"stderr", strings.TrimSpace(stderr.String()),
		)
	}

	// Empty stdout means "no changes" -- return the original input.
	output := stdout.String()
	if strings.TrimSpace(output) == "" {
		logger.Log(ctx, LevelTrace, "hook produced empty output, keeping original XML", "path", hookPath)
		return xmlInput, nil
	}

	logger.Log(ctx, LevelTrace, "hook produced modified XML",
		"path", hookPath,
		"inputLen", len(xmlInput),
		"outputLen", len(output),
	)
	return output, nil
}

// listHookDir reads a directory and returns the full paths of all executable
// regular files, sorted alphabetically by filename. This matches the behavior
// of libvirt's qemu.d/ hook directory (since libvirt 6.5.0).
//
// Non-regular files (directories, symlinks to directories, etc.) and
// non-executable files are silently skipped.
func listHookDir(dirPath string) ([]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", dirPath, err)
	}

	var hooks []string
	for _, entry := range entries {
		// Skip directories.
		if entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(dirPath, entry.Name())

		// Stat the file to check permissions (follows symlinks).
		info, err := os.Stat(fullPath)
		if err != nil {
			continue // Skip files we can't stat.
		}

		// Skip non-regular files.
		if !info.Mode().IsRegular() {
			continue
		}

		// Skip non-executable files.
		if info.Mode()&0111 == 0 {
			continue
		}

		hooks = append(hooks, fullPath)
	}

	// Sort alphabetically by full path. Since all files share the same
	// directory prefix, this is equivalent to sorting by filename.
	sort.Strings(hooks)

	return hooks, nil
}
