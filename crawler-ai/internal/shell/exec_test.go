package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	apperrors "crawler-ai/internal/errors"
)

func TestCommandsBlockerRejectsDangerousCommands(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"rm -rf build", "git reset --hard HEAD", "cmd /C del notes.txt"} {
		if !apperrors.IsCode(CommandsBlocker()(command), apperrors.CodePermissionDenied) {
			t.Fatalf("expected command %q to be blocked", command)
		}
	}
}

func TestExecTimesOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	command := "sleep 5"
	if runtime.GOOS == "windows" {
		command = "ping 127.0.0.1 -n 6 >NUL"
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	_, _, err := Exec(deadlineCtx, t.TempDir(), command)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRunTruncatesLargeOutput(t *testing.T) {
	t.Parallel()
	buffer := newCappedBuffer(MaxOutputBytes)
	_, err := buffer.Write([]byte(strings.Repeat("x", MaxOutputBytes+10)))
	if err != nil {
		t.Fatalf("unexpected buffer error: %v", err)
	}
	output := buffer.String()
	if !strings.Contains(output, "[output truncated at") {
		t.Fatalf("expected truncated output marker, got %q", output)
	}
}
