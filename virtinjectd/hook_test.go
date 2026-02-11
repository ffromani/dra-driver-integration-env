// AI-Attribution: AIA PAI Nc Hin R claude-4.6-opus-high v1.0
// SPDX-License-Identifier: LGPL-2.1-or-later

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLogger returns a quiet slog.Logger suitable for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(-100), // enable all levels
	}))
}

// writeScript creates an executable shell script at path with the given body.
func writeScript(t *testing.T, path, body string) {
	t.Helper()
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("writing script %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// NewHookChain
// ---------------------------------------------------------------------------

func TestNewHookChain_NeitherHookNorDir(t *testing.T) {
	_, err := NewHookChain(testLogger(), "", "", "")
	if err == nil {
		t.Fatal("expected error when both hookPath and hookDirPath are empty")
	}
}

func TestNewHookChain_HookPathDoesNotExist(t *testing.T) {
	_, err := NewHookChain(testLogger(), "/nonexistent/hook.sh", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent hook path")
	}
}

func TestNewHookChain_HookPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := NewHookChain(testLogger(), dir, "", "")
	if err == nil {
		t.Fatal("expected error when hookPath is a directory")
	}
}

func TestNewHookChain_HookPathNotExecutable(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(hookFile, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewHookChain(testLogger(), hookFile, "", "")
	if err == nil {
		t.Fatal("expected error when hook script is not executable")
	}
}

func TestNewHookChain_HookDirDoesNotExist(t *testing.T) {
	_, err := NewHookChain(testLogger(), "", "/nonexistent/hooks.d", "")
	if err == nil {
		t.Fatal("expected error for nonexistent hook directory")
	}
}

func TestNewHookChain_HookDirIsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewHookChain(testLogger(), "", filePath, "")
	if err == nil {
		t.Fatal("expected error when hookDirPath is a file")
	}
}

func TestNewHookChain_ValidHookOnly(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, "cat\n")

	hc, err := NewHookChain(testLogger(), hookFile, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hc.hookPath != hookFile {
		t.Errorf("hookPath = %q, want %q", hc.hookPath, hookFile)
	}
}

func TestNewHookChain_ValidDirOnly(t *testing.T) {
	dir := t.TempDir()
	hc, err := NewHookChain(testLogger(), "", dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hc.hookDirPath != dir {
		t.Errorf("hookDirPath = %q, want %q", hc.hookDirPath, dir)
	}
}

func TestNewHookChain_ValidBoth(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, "cat\n")

	hooksDir := t.TempDir()
	hc, err := NewHookChain(testLogger(), hookFile, hooksDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hc.hookPath != hookFile || hc.hookDirPath != hooksDir {
		t.Errorf("got hookPath=%q hookDirPath=%q", hc.hookPath, hc.hookDirPath)
	}
}

func TestNewHookChain_DefaultDomainName(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, "cat\n")

	hc, err := NewHookChain(testLogger(), hookFile, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hc.hookDomainName != defaultHookDomainName {
		t.Errorf("hookDomainName = %q, want %q", hc.hookDomainName, defaultHookDomainName)
	}
}

func TestNewHookChain_CustomDomainName(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, "cat\n")

	hc, err := NewHookChain(testLogger(), hookFile, "", "myvm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hc.hookDomainName != "myvm" {
		t.Errorf("hookDomainName = %q, want %q", hc.hookDomainName, "myvm")
	}
}

// ---------------------------------------------------------------------------
// runHook
// ---------------------------------------------------------------------------

func TestRunOneHook_Passthrough(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	// Script that copies stdin to stdout unchanged.
	writeScript(t, hookFile, "cat\n")

	input := "<domain>original</domain>"
	output, err := runHook(testLogger(), hookFile, "test", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != input {
		t.Errorf("got %q, want %q", output, input)
	}
}

func TestRunOneHook_ModifiesXML(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	// Script that replaces content.
	writeScript(t, hookFile, `echo "<domain>modified</domain>"`)

	input := "<domain>original</domain>"
	output, err := runHook(testLogger(), hookFile, "test", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "<domain>modified</domain>\n"
	if output != expected {
		t.Errorf("got %q, want %q", output, expected)
	}
}

func TestRunOneHook_EmptyStdout(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	// Script that produces no output → "no changes".
	writeScript(t, hookFile, "true\n")

	input := "<domain>keep-me</domain>"
	output, err := runHook(testLogger(), hookFile, "test", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != input {
		t.Errorf("got %q, want %q (original input)", output, input)
	}
}

func TestRunOneHook_NonZeroExit(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	// Script that fails with exit code 1.
	writeScript(t, hookFile, "exit 1\n")

	input := "<domain>unchanged</domain>"
	output, err := runHook(testLogger(), hookFile, "test", input)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	// On failure, original input should be returned.
	if output != input {
		t.Errorf("got %q, want %q (original input on error)", output, input)
	}
}

func TestRunOneHook_NonZeroExitWithStderr(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	// Script that writes to stderr and fails.
	writeScript(t, hookFile, "echo 'something went wrong' >&2; exit 1\n")

	input := "<domain/>"
	output, err := runHook(testLogger(), hookFile, "test", input)
	if err == nil {
		t.Fatal("expected error for non-zero exit with stderr")
	}
	if output != input {
		t.Errorf("got %q, want %q", output, input)
	}
}

func TestRunOneHook_StderrOnSuccess(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	// Script that writes a warning to stderr but succeeds and passes through.
	writeScript(t, hookFile, "echo 'warning: something' >&2; cat\n")

	input := "<domain>with-warning</domain>"
	output, err := runHook(testLogger(), hookFile, "test", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != input {
		t.Errorf("got %q, want %q", output, input)
	}
}

func TestRunOneHook_ReceivesDomainNameAsArgv1(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	// Script that echoes argv[1] (the domain name) to stdout.
	writeScript(t, hookFile, `echo "$1"`)

	output, err := runHook(testLogger(), hookFile, "my-sentinel", "<domain/>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(output) != "my-sentinel" {
		t.Errorf("hook got argv[1] = %q, want %q", strings.TrimSpace(output), "my-sentinel")
	}
}

// ---------------------------------------------------------------------------
// listHookDir
// ---------------------------------------------------------------------------

func TestListHookDir_Empty(t *testing.T) {
	dir := t.TempDir()
	hooks, err := listHookDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks, got %d", len(hooks))
	}
}

func TestListHookDir_SkipsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	// Non-executable file.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	hooks, err := listHookDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks (non-executable), got %d: %v", len(hooks), hooks)
	}
}

func TestListHookDir_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	hooks, err := listHookDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks (subdirectory skipped), got %d: %v", len(hooks), hooks)
	}
}

func TestListHookDir_SortedAlphabetically(t *testing.T) {
	dir := t.TempDir()
	// Create scripts out of alphabetical order.
	for _, name := range []string{"03-third.sh", "01-first.sh", "02-second.sh"} {
		writeScript(t, filepath.Join(dir, name), "true\n")
	}

	hooks, err := listHookDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(hooks))
	}

	expected := []string{
		filepath.Join(dir, "01-first.sh"),
		filepath.Join(dir, "02-second.sh"),
		filepath.Join(dir, "03-third.sh"),
	}
	for i, want := range expected {
		if hooks[i] != want {
			t.Errorf("hooks[%d] = %q, want %q", i, hooks[i], want)
		}
	}
}

func TestListHookDir_NonexistentDir(t *testing.T) {
	_, err := listHookDir("/nonexistent/dir")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestListHookDir_MixedContent(t *testing.T) {
	dir := t.TempDir()
	// Executable script (should be included).
	writeScript(t, filepath.Join(dir, "hook.sh"), "true\n")
	// Non-executable file (should be skipped).
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("x: 1"), 0644); err != nil {
		t.Fatal(err)
	}
	// Subdirectory (should be skipped).
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	hooks, err := listHookDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d: %v", len(hooks), hooks)
	}
	if filepath.Base(hooks[0]) != "hook.sh" {
		t.Errorf("expected hook.sh, got %q", hooks[0])
	}
}

// ---------------------------------------------------------------------------
// HookChain.Run
// ---------------------------------------------------------------------------

func TestHookChainRun_SingleHookPassthrough(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, "cat\n")

	hc, err := NewHookChain(testLogger(), hookFile, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := "<domain>test</domain>"
	output, err := hc.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != input {
		t.Errorf("got %q, want %q", output, input)
	}
}

func TestHookChainRun_SingleHookModifies(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, `echo "<domain>patched</domain>"`)

	hc, err := NewHookChain(testLogger(), hookFile, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := hc.Run("<domain>original</domain>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "<domain>patched</domain>\n"
	if output != expected {
		t.Errorf("got %q, want %q", output, expected)
	}
}

func TestHookChainRun_SingleHookFails(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, "exit 1\n")

	hc, err := NewHookChain(testLogger(), hookFile, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := "<domain>keep</domain>"
	output, err := hc.Run(input)
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	// On hook failure, original XML is preserved.
	if output != input {
		t.Errorf("got %q, want %q (original on failure)", output, input)
	}
}

func TestHookChainRun_DirHooksChained(t *testing.T) {
	hooksDir := t.TempDir()

	// First hook: append "-a".
	writeScript(t, filepath.Join(hooksDir, "01-a.sh"),
		`read input; echo "${input}-a"`)
	// Second hook: append "-b".
	writeScript(t, filepath.Join(hooksDir, "02-b.sh"),
		`read input; echo "${input}-b"`)

	hc, err := NewHookChain(testLogger(), "", hooksDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := hc.Run("start")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// read strips the trailing newline, so chaining produces "start-a-b\n".
	expected := "start-a-b\n"
	if output != expected {
		t.Errorf("got %q, want %q", output, expected)
	}
}

func TestHookChainRun_DirHookFailureContinues(t *testing.T) {
	hooksDir := t.TempDir()

	// First hook: fails.
	writeScript(t, filepath.Join(hooksDir, "01-fail.sh"), "exit 1\n")
	// Second hook: succeeds, outputs modified XML.
	writeScript(t, filepath.Join(hooksDir, "02-ok.sh"),
		`echo "<domain>from-second</domain>"`)

	hc, err := NewHookChain(testLogger(), "", hooksDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := hc.Run("<domain>input</domain>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The second hook should still run and produce its output.
	expected := "<domain>from-second</domain>\n"
	if output != expected {
		t.Errorf("got %q, want %q", output, expected)
	}
}

func TestHookChainRun_BothHookAndDir(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, `echo "after-single-hook"`)

	hooksDir := t.TempDir()
	writeScript(t, filepath.Join(hooksDir, "01.sh"),
		`read input; echo "${input}-plus-dir"`)

	hc, err := NewHookChain(testLogger(), hookFile, hooksDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := hc.Run("initial")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// read strips the trailing newline from the single hook's output.
	expected := "after-single-hook-plus-dir\n"
	if output != expected {
		t.Errorf("got %q, want %q", output, expected)
	}
}

func TestHookChainRun_DirListError(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "hook.sh")
	writeScript(t, hookFile, "cat\n")

	// Point hookDirPath to a nonexistent directory. We have to build the
	// struct directly because NewHookChain validates.
	hc := &HookChain{
		hookPath:       "",
		hookDirPath:    "/nonexistent/hooks.d",
		hookDomainName: defaultHookDomainName,
		logger:         testLogger().With("component", "hooks"),
	}

	_, err := hc.Run("<domain/>")
	if err == nil {
		t.Fatal("expected error for nonexistent hook directory during Run")
	}
}
