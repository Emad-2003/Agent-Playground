package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"
)

const (
	DefaultShellTimeout = 30 * time.Second
	MaxOutputBytes      = 64 * 1024
)

type BlockFunc func(command string) error

var blockedStandaloneCommands = map[string]struct{}{
	"dd":       {},
	"del":      {},
	"diskpart": {},
	"erase":    {},
	"format":   {},
	"halt":     {},
	"mkfs":     {},
	"poweroff": {},
	"reboot":   {},
	"rm":       {},
	"rmdir":    {},
	"shutdown": {},
	"taskkill": {},
}

var blockedCommandSequences = [][]string{
	{"git", "clean"},
	{"git", "checkout", "--"},
	{"git", "reset", "--hard"},
	{"git", "restore", "--source"},
	{"powershell", "remove-item"},
	{"pwsh", "remove-item"},
	{"reg", "delete"},
	{"sc", "delete"},
}

var safeCommands = []string{
	"cal",
	"date",
	"df",
	"du",
	"echo",
	"env",
	"free",
	"groups",
	"hostname",
	"id",
	"kill",
	"killall",
	"ls",
	"nice",
	"nohup",
	"printenv",
	"ps",
	"pwd",
	"set",
	"time",
	"timeout",
	"top",
	"type",
	"uname",
	"unset",
	"uptime",
	"whatis",
	"whereis",
	"which",
	"whoami",
	"git blame",
	"git branch",
	"git config --get",
	"git config --list",
	"git describe",
	"git diff",
	"git grep",
	"git log",
	"git ls-files",
	"git ls-remote",
	"git remote",
	"git rev-parse",
	"git shortlog",
	"git show",
	"git status",
	"git tag",
}

func init() {
	if runtime.GOOS == "windows" {
		safeCommands = append(safeCommands, "ipconfig", "nslookup", "ping", "systeminfo", "tasklist", "where")
	}
}

type cappedBuffer struct {
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func newCappedBuffer(maxBytes int) *cappedBuffer {
	return &cappedBuffer{maxBytes: maxBytes}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.maxBytes <= 0 {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.maxBytes - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string {
	text := strings.TrimSpace(b.buf.String())
	if b.truncated {
		if text != "" {
			text += "\n"
		}
		text += fmt.Sprintf("[output truncated at %d bytes]", b.maxBytes)
	}
	return text
}

func CommandsBlocker() BlockFunc {
	return func(command string) error {
		tokens := tokenizeCommand(command)
		for _, token := range tokens {
			if _, blocked := blockedStandaloneCommands[token]; blocked {
				return apperrors.New("shell.CommandsBlocker", apperrors.CodePermissionDenied, "shell command blocked by safety policy")
			}
		}
		for _, sequence := range blockedCommandSequences {
			if containsTokenSequence(tokens, sequence) {
				return apperrors.New("shell.CommandsBlocker", apperrors.CodePermissionDenied, "shell command blocked by safety policy")
			}
		}
		return nil
	}
}

func IsSafeReadOnly(command string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(command))
	if trimmed == "" {
		return false
	}
	for _, safe := range safeCommands {
		if strings.HasPrefix(trimmed, safe) {
			if len(trimmed) == len(safe) || trimmed[len(safe)] == ' ' || trimmed[len(safe)] == '-' {
				return true
			}
		}
	}
	return false
}

func Run(ctx context.Context, workingDir, command string) (string, error) {
	stdout, stderr, err := Exec(ctx, workingDir, command)
	output := formatOutput(stdout, stderr)
	if err != nil {
		return output, err
	}
	return output, nil
}

func Exec(ctx context.Context, workingDir, command string) (string, string, error) {
	if strings.TrimSpace(command) == "" {
		return "", "", apperrors.New("shell.Exec", apperrors.CodeInvalidArgument, "shell command must not be empty")
	}
	if err := CommandsBlocker()(command); err != nil {
		return "", "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, DefaultShellTimeout)
	defer cancel()

	stdout := newCappedBuffer(MaxOutputBytes)
	stderr := newCappedBuffer(MaxOutputBytes)
	if err := execWithStreams(requestCtx, workingDir, command, stdout, stderr); err != nil {
		if errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return stdout.String(), stderr.String(), apperrors.New("shell.Exec", apperrors.CodeToolFailed, fmt.Sprintf("shell command timed out after %s", DefaultShellTimeout))
		}
		return stdout.String(), stderr.String(), apperrors.Wrap("shell.Exec", apperrors.CodeToolFailed, err, "execute shell command")
	}
	return stdout.String(), stderr.String(), nil
}

func execWithStreams(ctx context.Context, workingDir, command string, stdout, stderr io.Writer) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = workingDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func formatOutput(stdout, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	parts := make([]string, 0, 2)
	if stdout != "" {
		parts = append(parts, stdout)
	}
	if stderr != "" {
		parts = append(parts, "stderr:\n"+stderr)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func tokenizeCommand(command string) []string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	return slices.DeleteFunc(fields, func(token string) bool { return token == "" })
}

func containsTokenSequence(tokens []string, sequence []string) bool {
	if len(sequence) == 0 || len(tokens) < len(sequence) {
		return false
	}
	for index := 0; index <= len(tokens)-len(sequence); index++ {
		matched := true
		for offset := range sequence {
			if tokens[index+offset] != sequence[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
