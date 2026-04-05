package commands

import (
	"testing"

	apperrors "crawler-ai/internal/errors"
)

func TestParsePromptReadCommand(t *testing.T) {
	parsed, err := ParsePrompt("/read README.md")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Kind != KindTool || parsed.Tool.Name != "read_file" || parsed.Tool.Path != "README.md" {
		t.Fatalf("unexpected parsed command: %+v", parsed)
	}
}

func TestParsePromptViewCommand(t *testing.T) {
	parsed, err := ParsePrompt("/view README.md::10::20")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Kind != KindTool || parsed.Tool.Name != "view" || parsed.Tool.Path != "README.md" || parsed.Tool.StartLine != 10 || parsed.Tool.Limit != 20 {
		t.Fatalf("unexpected parsed view command: %+v", parsed)
	}
	parsed, err = ParsePrompt("/view README.md")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Tool.Name != "view" || parsed.Tool.Path != "README.md" || parsed.Tool.StartLine != 0 || parsed.Tool.Limit != 0 {
		t.Fatalf("unexpected default parsed view command: %+v", parsed)
	}
}

func TestParsePromptGlobCommand(t *testing.T) {
	parsed, err := ParsePrompt("/glob src/**/*.go")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Kind != KindTool || parsed.Tool.Name != "glob" || parsed.Tool.Pattern != "src/**/*.go" {
		t.Fatalf("unexpected parsed glob command: %+v", parsed)
	}
}

func TestParsePromptFetchCommand(t *testing.T) {
	parsed, err := ParsePrompt("/fetch https://example.com")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Kind != KindTool || parsed.Tool.Name != "fetch" || parsed.Tool.URL != "https://example.com" {
		t.Fatalf("unexpected parsed fetch command: %+v", parsed)
	}
}

func TestParsePromptWriteCommand(t *testing.T) {
	parsed, err := ParsePrompt("/write notes.txt::hello")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Tool.Name != "write_file" || parsed.Tool.Path != "notes.txt" || parsed.Tool.Content != "hello" {
		t.Fatalf("unexpected parsed write command: %+v", parsed)
	}
}

func TestParsePromptEditCommand(t *testing.T) {
	parsed, err := ParsePrompt("/edit notes.txt::hello::goodbye")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Tool.Name != "edit_file" || parsed.Tool.Path != "notes.txt" || parsed.Tool.OldText != "hello" || parsed.Tool.NewText != "goodbye" {
		t.Fatalf("unexpected parsed edit command: %+v", parsed)
	}
}

func TestParsePromptBackgroundShellAndJobs(t *testing.T) {
	parsed, err := ParsePrompt("/shell-bg echo hello")
	if err != nil || parsed.Tool.Name != "shell_bg" || parsed.Tool.Command != "echo hello" {
		t.Fatalf("unexpected parsed background shell command: %+v err=%v", parsed, err)
	}
	parsed, err = ParsePrompt("/job-output job-001")
	if err != nil || parsed.Tool.Name != "job_output" || parsed.Tool.JobID != "job-001" {
		t.Fatalf("unexpected parsed job-output command: %+v err=%v", parsed, err)
	}
	parsed, err = ParsePrompt("/job-kill job-001")
	if err != nil || parsed.Tool.Name != "job_kill" || parsed.Tool.JobID != "job-001" {
		t.Fatalf("unexpected parsed job-kill command: %+v err=%v", parsed, err)
	}
}

func TestParsePromptRejectsMalformedWrite(t *testing.T) {
	_, err := ParsePrompt("/write notes.txt")
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}

func TestParsePromptRejectsMalformedEdit(t *testing.T) {
	_, err := ParsePrompt("/edit notes.txt::hello")
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}

func TestParsePromptRejectsMalformedView(t *testing.T) {
	_, err := ParsePrompt("/view notes.txt::0::20")
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
	_, err = ParsePrompt("/view notes.txt::10")
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}
