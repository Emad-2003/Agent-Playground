package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	apperrors "crawler-ai/internal/errors"
)

type Result struct {
	Output string
	Path   string
}

type Executor struct {
	workspaceRoot string
}

func NewExecutor(workspaceRoot string) (*Executor, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, apperrors.New("tools.NewExecutor", apperrors.CodeInvalidArgument, "workspace root must not be empty")
	}

	return &Executor{workspaceRoot: filepath.Clean(workspaceRoot)}, nil
}

func (e *Executor) ReadFile(path string) (Result, error) {
	resolved, relative, err := e.resolvePath(path)
	if err != nil {
		return Result{}, err
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.ReadFile", apperrors.CodeToolFailed, err, "read file")
	}

	return Result{Output: string(content), Path: relative}, nil
}

func (e *Executor) WriteFile(path, content string) (Result, error) {
	resolved, relative, err := e.resolvePath(path)
	if err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return Result{}, apperrors.Wrap("tools.WriteFile", apperrors.CodeToolFailed, err, "create parent directories")
	}

	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return Result{}, apperrors.Wrap("tools.WriteFile", apperrors.CodeToolFailed, err, "write file")
	}

	return Result{Output: fmt.Sprintf("wrote %d bytes", len(content)), Path: relative}, nil
}

func (e *Executor) ListFiles(path string) (Result, error) {
	resolved, _, err := e.resolvePath(defaultPath(path))
	if err != nil {
		return Result{}, err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.ListFiles", apperrors.CodeToolFailed, err, "list directory")
	}

	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += string(os.PathSeparator)
		}
		items = append(items, name)
	}

	return Result{Output: strings.Join(items, "\n")}, nil
}

func (e *Executor) Grep(pattern string) (Result, error) {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.Grep", apperrors.CodeInvalidArgument, err, "compile grep pattern")
	}

	matches := make([]string, 0)
	err = filepath.WalkDir(e.workspaceRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lines := strings.Split(string(content), "\n")
		for index, line := range lines {
			if compiled.MatchString(line) {
				relative, relErr := filepath.Rel(e.workspaceRoot, path)
				if relErr != nil {
					return relErr
				}
				matches = append(matches, fmt.Sprintf("%s:%d:%s", relative, index+1, line))
			}
		}

		return nil
	})
	if err != nil {
		return Result{}, apperrors.Wrap("tools.Grep", apperrors.CodeToolFailed, err, "search workspace")
	}

	return Result{Output: strings.Join(matches, "\n")}, nil
}

func (e *Executor) RunShell(ctx context.Context, command string) (Result, error) {
	if strings.TrimSpace(command) == "" {
		return Result{}, apperrors.New("tools.RunShell", apperrors.CodeInvalidArgument, "shell command must not be empty")
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = e.workspaceRoot

	output, err := cmd.CombinedOutput()
	result := Result{Output: strings.TrimSpace(string(output))}
	if err != nil {
		return result, apperrors.Wrap("tools.RunShell", apperrors.CodeToolFailed, err, "execute shell command")
	}

	return result, nil
}

func (e *Executor) resolvePath(path string) (string, string, error) {
	target := strings.TrimSpace(path)
	if target == "" {
		return "", "", apperrors.New("tools.resolvePath", apperrors.CodeInvalidArgument, "path must not be empty")
	}

	resolved := target
	if !filepath.IsAbs(target) {
		resolved = filepath.Join(e.workspaceRoot, target)
	}
	resolved = filepath.Clean(resolved)

	relative, err := filepath.Rel(e.workspaceRoot, resolved)
	if err != nil {
		return "", "", apperrors.Wrap("tools.resolvePath", apperrors.CodePathViolation, err, "resolve path against workspace")
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", apperrors.New("tools.resolvePath", apperrors.CodePathViolation, "path escapes workspace root")
	}

	return resolved, relative, nil
}

func defaultPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return path
}
