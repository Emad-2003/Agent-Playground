package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/fsext"
)

const (
	defaultViewLineLimit = 200
	maxViewFileBytes     = 1 * 1024 * 1024
	maxViewLineLength    = 2000
	maxGlobResults       = 100
	lineNumberWidth      = 6
)

func (e *Executor) Glob(ctx context.Context, pattern string) (Result, error) {
	normalized := filepath.ToSlash(strings.TrimSpace(pattern))
	if normalized == "" {
		return Result{}, apperrors.New("tools.Glob", apperrors.CodeInvalidArgument, "glob pattern must not be empty")
	}
	searchCtx, cancel := context.WithTimeout(ctx, defaultSearchTimeout)
	defer cancel()

	var (
		matches   []string
		truncated bool
		err       error
	)
	if cmd := getRgCmd(searchCtx, e.workspaceRoot, normalized); cmd != nil {
		matches, truncated, err = runRipgrep(cmd, e.workspaceRoot, maxGlobResults)
	}
	if err != nil || matches == nil {
		matches, truncated, err = GlobGitignoreAware(normalized, e.workspaceRoot, maxGlobResults)
	}
	if err != nil {
		return Result{}, apperrors.Wrap("tools.Glob", apperrors.CodeToolFailed, err, "glob workspace files")
	}

	output := "No files found"
	if len(matches) > 0 {
		output = strings.Join(matches, "\n")
		if truncated {
			output += "\n\n(Results are truncated. Consider using a more specific pattern.)"
		}
	}

	return Result{
		Output: output,
		Extra: map[string]string{
			"pattern":   normalized,
			"count":     strconv.Itoa(len(matches)),
			"truncated": strconv.FormatBool(truncated),
		},
	}, nil
}

func runRipgrep(cmd *exec.Cmd, searchRoot string, limit int) ([]string, bool, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, false, nil
		}
		return nil, false, err
	}
	var matches []string
	for value := range bytes.SplitSeq(out, []byte{0}) {
		if len(value) == 0 {
			continue
		}
		matchedPath := string(value)
		if !filepath.IsAbs(matchedPath) {
			matchedPath = filepath.Join(searchRoot, matchedPath)
		}
		relative, err := filepath.Rel(searchRoot, matchedPath)
		if err != nil {
			continue
		}
		matches = append(matches, filepath.ToSlash(relative))
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if len(matches[i]) == len(matches[j]) {
			return matches[i] < matches[j]
		}
		return len(matches[i]) < len(matches[j])
	})
	truncated := limit > 0 && len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	return matches, truncated, nil
}

func (e *Executor) View(pathValue string, startLine, limit int) (Result, error) {
	if startLine <= 0 {
		startLine = 1
	}
	if limit <= 0 {
		limit = defaultViewLineLimit
	}

	resolved, relative, err := fsext.ResolveWithinWorkspace(e.workspaceRoot, pathValue)
	if err != nil {
		return Result{}, err
	}
	displayPath := filepath.ToSlash(relative)
	info, err := os.Stat(resolved)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.View", apperrors.CodeToolFailed, err, "stat file for view")
	}
	if info.IsDir() {
		return Result{}, apperrors.New("tools.View", apperrors.CodeToolFailed, "view target must be a file, not a directory")
	}
	if info.Size() > maxViewFileBytes {
		return Result{}, apperrors.New("tools.View", apperrors.CodeToolFailed, fmt.Sprintf("file is too large to view safely (%d bytes; limit is %d)", info.Size(), maxViewFileBytes))
	}

	content, hasMore, linesRead, err := readViewRange(resolved, startLine, limit)
	if err != nil {
		return Result{}, err
	}

	output := "<file>\n"
	if strings.TrimSpace(content) != "" {
		output += addLineNumbers(content, startLine) + "\n"
	}
	output += "</file>"
	if hasMore {
		output += fmt.Sprintf("\n\n(File has more lines. Use /view %s::%d::%d to continue.)", displayPath, startLine+linesRead, limit)
	}

	return Result{
		Output: output,
		Path:   displayPath,
		Extra: map[string]string{
			"start_line": strconv.Itoa(startLine),
			"lines":      strconv.Itoa(linesRead),
			"has_more":   strconv.FormatBool(hasMore),
		},
	}, nil
}

func readViewRange(filePath string, startLine, limit int) (string, bool, int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", false, 0, apperrors.Wrap("tools.readViewRange", apperrors.CodeToolFailed, err, "open file for view")
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, maxViewFileBytes)

	toSkip := startLine - 1
	for skipped := 0; skipped < toSkip && scanner.Scan(); skipped++ {
	}

	lines := make([]string, 0, limit)
	for len(lines) < limit && scanner.Scan() {
		line := scanner.Text()
		if !utf8.ValidString(line) {
			return "", false, 0, apperrors.New("tools.readViewRange", apperrors.CodeToolFailed, "file content is not valid UTF-8")
		}
		if len(line) > maxViewLineLength {
			line = line[:maxViewLineLength] + "..."
		}
		lines = append(lines, strings.TrimSuffix(line, "\r"))
	}
	hasMore := len(lines) == limit && scanner.Scan()
	if err := scanner.Err(); err != nil {
		return "", false, 0, apperrors.Wrap("tools.readViewRange", apperrors.CodeToolFailed, err, "scan file for view")
	}
	return strings.Join(lines, "\n"), hasMore, len(lines), nil
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	for index, line := range lines {
		result = append(result, fmt.Sprintf("%*d|%s", lineNumberWidth, startLine+index, line))
	}
	return strings.Join(result, "\n")
}
