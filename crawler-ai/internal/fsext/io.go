package fsext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"
)

const managedHistoryDir = ".crawler-ai/history"

func ResolveWithinWorkspace(workspaceRoot, path string) (string, string, error) {
	target := strings.TrimSpace(path)
	if target == "" {
		return "", "", apperrors.New("fsext.ResolveWithinWorkspace", apperrors.CodeInvalidArgument, "path must not be empty")
	}

	resolved := target
	if !filepath.IsAbs(target) {
		resolved = filepath.Join(workspaceRoot, target)
	}
	resolved = filepath.Clean(resolved)

	relative, err := filepath.Rel(workspaceRoot, resolved)
	if err != nil {
		return "", "", apperrors.Wrap("fsext.ResolveWithinWorkspace", apperrors.CodePathViolation, err, "resolve path against workspace")
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", apperrors.New("fsext.ResolveWithinWorkspace", apperrors.CodePathViolation, "path escapes workspace root")
	}

	return resolved, relative, nil
}

func ReadFile(workspaceRoot, path string) ([]byte, string, error) {
	resolved, relative, err := ResolveWithinWorkspace(workspaceRoot, path)
	if err != nil {
		return nil, "", err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", apperrors.Wrap("fsext.ReadFile", apperrors.CodeToolFailed, err, "read file")
	}
	return content, relative, nil
}

func WriteFile(workspaceRoot, path string, content []byte) (string, error) {
	resolved, relative, err := ResolveWithinWorkspace(workspaceRoot, path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(resolved); err == nil {
		return "", apperrors.New("fsext.WriteFile", apperrors.CodeToolFailed, "file already exists; use edit_file for updates")
	} else if !os.IsNotExist(err) {
		return "", apperrors.Wrap("fsext.WriteFile", apperrors.CodeToolFailed, err, "stat target file")
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", apperrors.Wrap("fsext.WriteFile", apperrors.CodeToolFailed, err, "create parent directories")
	}
	if err := os.WriteFile(resolved, content, 0o644); err != nil {
		return "", apperrors.Wrap("fsext.WriteFile", apperrors.CodeToolFailed, err, "write file")
	}
	return relative, nil
}

func EditFile(workspaceRoot, path string, expectedOldContent, newContent []byte) (string, string, error) {
	resolved, relative, err := ResolveWithinWorkspace(workspaceRoot, path)
	if err != nil {
		return "", "", err
	}
	current, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", apperrors.New("fsext.EditFile", apperrors.CodeToolFailed, "file does not exist; use write_file to create it")
		}
		return "", "", apperrors.Wrap("fsext.EditFile", apperrors.CodeToolFailed, err, "read file before edit")
	}
	if string(current) != string(expectedOldContent) {
		return "", "", apperrors.New("fsext.EditFile", apperrors.CodeToolFailed, "file content changed since it was last read; refresh and retry edit")
	}
	historyRelative, err := writeHistorySnapshot(workspaceRoot, relative, current)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(resolved, newContent, 0o644); err != nil {
		return "", "", apperrors.Wrap("fsext.EditFile", apperrors.CodeToolFailed, err, "write edited file")
	}
	return relative, historyRelative, nil
}

func writeHistorySnapshot(workspaceRoot, relative string, content []byte) (string, error) {
	sanitized := filepath.Clean(relative)
	historyRelative := filepath.Join(managedHistoryDir, sanitized+"."+time.Now().UTC().Format("20060102T150405.000000000Z")+".bak")
	historyPath := filepath.Join(workspaceRoot, historyRelative)
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		return "", apperrors.Wrap("fsext.writeHistorySnapshot", apperrors.CodeToolFailed, err, "create history directory")
	}
	if err := os.WriteFile(historyPath, content, 0o644); err != nil {
		return "", apperrors.Wrap("fsext.writeHistorySnapshot", apperrors.CodeToolFailed, err, fmt.Sprintf("write history snapshot for %s", relative))
	}
	return historyRelative, nil
}
