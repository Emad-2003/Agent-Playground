package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	apperrors "crawler-ai/internal/errors"
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
	if readResult.Output != "hello world" {
		t.Fatalf("expected hello world, got %q", readResult.Output)
	}

	listResult, err := executor.ListFiles("notes")
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if !strings.Contains(listResult.Output, "todo.txt") {
		t.Fatalf("expected list output to contain todo.txt, got %q", listResult.Output)
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

	result, err := executor.Grep("alpha")
	if err != nil {
		t.Fatalf("unexpected grep error: %v", err)
	}
	if !strings.Contains(result.Output, "src") {
		t.Fatalf("expected grep output to contain file path, got %q", result.Output)
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
