package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/shell"
)

func TestWriteReadAndListFiles(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	writeResult, err := executor.WriteFile("notes/todo.txt", "hello world")
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if writeResult.Path != filepath.Join("notes", "todo.txt") {
		t.Fatalf("unexpected write path %s", writeResult.Path)
	}

	readResult, err := executor.ReadFile("notes/todo.txt")
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if readResult.Path != filepath.ToSlash(filepath.Join("notes", "todo.txt")) {
		t.Fatalf("unexpected read path %s", readResult.Path)
	}
	if !strings.Contains(readResult.Output, "<file>") || !strings.Contains(readResult.Output, "     1|hello world") {
		t.Fatalf("expected bounded read output, got %q", readResult.Output)
	}

	listResult, err := executor.ListFiles("notes")
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if !strings.Contains(listResult.Output, "todo.txt") {
		t.Fatalf("expected list output to contain todo.txt, got %q", listResult.Output)
	}
}

func TestWriteFileRejectsOverwrite(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if _, err := executor.WriteFile("notes/todo.txt", "hello world"); err != nil {
		t.Fatalf("unexpected initial write error: %v", err)
	}
	if _, err := executor.WriteFile("notes/todo.txt", "changed"); err == nil {
		t.Fatal("expected overwrite to be rejected")
	}
}

func TestEditFileReplacesExactContentAndWritesHistory(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	executor, err := NewExecutor(workspace)
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if _, err := executor.WriteFile("notes/todo.txt", "hello world"); err != nil {
		t.Fatalf("unexpected initial write error: %v", err)
	}

	result, err := executor.EditFile("notes/todo.txt", "hello world", "hello crawler")
	if err != nil {
		t.Fatalf("unexpected edit error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "notes", "todo.txt"))
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(content) != "hello crawler" {
		t.Fatalf("expected edited content, got %q", string(content))
	}
	historyPath := result.Extra["history_path"]
	if historyPath == "" {
		t.Fatalf("expected history path in result, got %#v", result)
	}
	historyContent, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(historyPath)))
	if err != nil {
		t.Fatalf("expected history snapshot to be readable, got %v", err)
	}
	if string(historyContent) != "hello world" {
		t.Fatalf("expected history snapshot content, got %q", string(historyContent))
	}
	if !strings.Contains(result.Output, historyPath) {
		t.Fatalf("expected result output to mention history path, got %q", result.Output)
	}
}

func TestReadFileUsesBoundedViewSemantics(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	lines := make([]string, 0, defaultViewLineLimit+5)
	for index := 1; index <= defaultViewLineLimit+5; index++ {
		lines = append(lines, "line "+strconv.Itoa(index))
	}
	if _, err := executor.WriteFile("notes/long.txt", strings.Join(lines, "\n")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	result, err := executor.ReadFile("notes/long.txt")
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if !strings.Contains(result.Output, "Use /view notes/long.txt::201::200 to continue") {
		t.Fatalf("expected read continuation hint, got %q", result.Output)
	}
	if result.Extra["has_more"] != "true" {
		t.Fatalf("expected has_more metadata, got %#v", result.Extra)
	}
}

func TestEditFileRejectsStaleContent(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if _, err := executor.WriteFile("notes/todo.txt", "hello world"); err != nil {
		t.Fatalf("unexpected initial write error: %v", err)
	}
	if _, err := executor.EditFile("notes/todo.txt", "something else", "hello crawler"); err == nil {
		t.Fatal("expected stale edit to be rejected")
	}
}

func TestRejectsPathEscape(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	_, err = executor.WriteFile("..\\escape.txt", "bad")
	if err == nil {
		t.Fatal("expected path violation error")
	}
	if !apperrors.IsCode(err, apperrors.CodePathViolation) {
		t.Fatalf("expected path violation error, got %v", err)
	}
}

func TestGrepFindsMatches(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	if _, err := executor.WriteFile("src/app.txt", "alpha\nbeta\nalpha beta"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	result, err := executor.Grep(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("unexpected grep error: %v", err)
	}
	if !strings.Contains(result.Output, "src/app.txt") || !strings.Contains(result.Output, "Line 1") {
		t.Fatalf("expected grep output to contain file path, got %q", result.Output)
	}
}

func TestGrepRespectsGitignore(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if _, err := executor.WriteFile("visible/app.txt", "alpha visible"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if _, err := executor.WriteFile("ignored/app.txt", "alpha ignored"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(executor.workspaceRoot, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatalf("unexpected ignore write error: %v", err)
	}

	result, err := executor.Grep(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("unexpected grep error: %v", err)
	}
	if !strings.Contains(result.Output, "visible/app.txt") {
		t.Fatalf("expected visible file in grep output, got %q", result.Output)
	}
	if strings.Contains(result.Output, "ignored/app.txt") {
		t.Fatalf("expected ignored file to be excluded, got %q", result.Output)
	}
}

func TestGlobFindsRecursiveMatches(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if _, err := executor.WriteFile("src/app/main.go", "package main"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if _, err := executor.WriteFile("src/app/main_test.go", "package main"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if _, err := executor.WriteFile("docs/readme.md", "hello"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	result, err := executor.Glob(context.Background(), "src/**/*.go")
	if err != nil {
		t.Fatalf("unexpected glob error: %v", err)
	}
	if !strings.Contains(result.Output, "src/app/main.go") || !strings.Contains(result.Output, "src/app/main_test.go") {
		t.Fatalf("expected recursive glob matches, got %q", result.Output)
	}
	if strings.Contains(result.Output, "docs/readme.md") {
		t.Fatalf("expected markdown file to be excluded, got %q", result.Output)
	}
}

func TestGlobRespectsGitignore(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if _, err := executor.WriteFile("visible/main.go", "package main"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if _, err := executor.WriteFile("ignored/main.go", "package main"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(executor.workspaceRoot, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatalf("unexpected ignore write error: %v", err)
	}

	result, err := executor.Glob(context.Background(), "**/*.go")
	if err != nil {
		t.Fatalf("unexpected glob error: %v", err)
	}
	if !strings.Contains(result.Output, "visible/main.go") {
		t.Fatalf("expected visible file in glob output, got %q", result.Output)
	}
	if strings.Contains(result.Output, "ignored/main.go") {
		t.Fatalf("expected ignored file to be excluded, got %q", result.Output)
	}
}

func TestViewReturnsLineNumberedRange(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if _, err := executor.WriteFile("notes.txt", "first\nsecond\nthird\nfourth"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	result, err := executor.View("notes.txt", 2, 2)
	if err != nil {
		t.Fatalf("unexpected view error: %v", err)
	}
	if result.Path != "notes.txt" {
		t.Fatalf("expected view path to be preserved, got %#v", result)
	}
	if !strings.Contains(result.Output, "     2|second") || !strings.Contains(result.Output, "     3|third") {
		t.Fatalf("expected numbered view output, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "Use /view notes.txt::4::2 to continue") {
		t.Fatalf("expected continuation hint, got %q", result.Output)
	}
	if result.Extra["has_more"] != "true" {
		t.Fatalf("expected has_more metadata, got %#v", result.Extra)
	}
}

func TestRunShell(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	result, err := executor.RunShell(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("unexpected shell error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result.Output), "hello") {
		t.Fatalf("expected shell output to contain hello, got %q", result.Output)
	}
}

func TestFetchReturnsNormalizedHTML(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h1>Hello</h1><script>ignored()</script><p>world</p></body></html>"))
	}))
	defer server.Close()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	result, err := executor.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}
	if !strings.Contains(result.Output, "Hello") || !strings.Contains(result.Output, "world") {
		t.Fatalf("expected normalized html text, got %q", result.Output)
	}
	if strings.Contains(result.Output, "ignored") {
		t.Fatalf("expected script content to be excluded, got %q", result.Output)
	}
}

func TestFetchTruncatesLargeResponses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", largeFetchThreshold+100)))
	}))
	defer server.Close()

	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	result, err := executor.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}
	if !strings.Contains(result.Output, "Content saved to:") || !strings.Contains(result.Output, "Use the view and grep tools to analyze this file.") {
		t.Fatalf("expected file-backed large fetch output, got %q", result.Output)
	}
	savedPath := result.Extra["saved_path"]
	if savedPath == "" {
		t.Fatalf("expected saved_path metadata, got %#v", result.Extra)
	}
	content, err := os.ReadFile(filepath.Join(executor.workspaceRoot, filepath.FromSlash(savedPath)))
	if err != nil {
		t.Fatalf("expected saved fetch content file, got %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected saved fetch content to be non-empty")
	}
}

func TestBackgroundShellLifecycle(t *testing.T) {
	executor, err := NewExecutor(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	result, err := executor.RunBackgroundShell(context.Background(), "echo hello background")
	if err != nil {
		t.Fatalf("unexpected background shell error: %v", err)
	}
	jobID := result.Extra["job_id"]
	if jobID == "" {
		t.Fatalf("expected background job id, got %#v", result)
	}
	output, err := executor.GetBackgroundShellOutput(jobID)
	if err != nil {
		t.Fatalf("unexpected job output error: %v", err)
	}
	if !strings.Contains(output.Output, jobID) {
		t.Fatalf("expected job output to mention job id, got %q", output.Output)
	}
	if err := shell.GetBackgroundJobManager().Kill(jobID); err != nil {
		t.Fatalf("unexpected cleanup kill error: %v", err)
	}
}
