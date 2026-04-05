package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"

	apperrors "crawler-ai/internal/errors"
)

const (
	defaultSearchTimeout = 30 * time.Second
	maxGrepResults       = 100
	maxGrepContentWidth  = 500
)

type grepMatch struct {
	path     string
	modTime  time.Time
	lineNum  int
	charNum  int
	lineText string
}

type ripgrepMatch struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

func (e *Executor) Grep(ctx context.Context, pattern string) (Result, error) {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return Result{}, apperrors.New("tools.Grep", apperrors.CodeInvalidArgument, "grep pattern must not be empty")
	}
	searchCtx, cancel := context.WithTimeout(ctx, defaultSearchTimeout)
	defer cancel()

	matches, truncated, err := searchFiles(searchCtx, trimmed, e.workspaceRoot, "", maxGrepResults)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.Grep", apperrors.CodeToolFailed, err, "search workspace")
	}

	var output strings.Builder
	if len(matches) == 0 {
		output.WriteString("No files found")
	} else {
		fmt.Fprintf(&output, "Found %d matches\n", len(matches))
		currentFile := ""
		for _, match := range matches {
			if currentFile != match.path {
				if currentFile != "" {
					output.WriteString("\n")
				}
				currentFile = match.path
				fmt.Fprintf(&output, "%s:\n", currentFile)
			}
			lineText := match.lineText
			if len(lineText) > maxGrepContentWidth {
				lineText = lineText[:maxGrepContentWidth] + "..."
			}
			if match.charNum > 0 {
				fmt.Fprintf(&output, "  Line %d, Char %d: %s\n", match.lineNum, match.charNum, lineText)
			} else {
				fmt.Fprintf(&output, "  Line %d: %s\n", match.lineNum, lineText)
			}
		}
		if truncated {
			output.WriteString("\n(Results are truncated. Consider using a more specific path or pattern.)")
		}
	}

	return Result{Output: strings.TrimSpace(output.String()), Extra: map[string]string{"count": fmt.Sprintf("%d", len(matches)), "truncated": fmt.Sprintf("%t", truncated)}}, nil
}

func searchFiles(ctx context.Context, pattern, rootPath, include string, limit int) ([]grepMatch, bool, error) {
	matches, err := searchWithRipgrep(ctx, pattern, rootPath, include)
	if err != nil {
		matches, err = searchFilesWithRegex(pattern, rootPath, include)
		if err != nil {
			return nil, false, err
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	return matches, truncated, nil
}

func searchWithRipgrep(ctx context.Context, pattern, rootPath, include string) ([]grepMatch, error) {
	cmd := getRgSearchCmd(ctx, pattern, rootPath, include)
	if cmd == nil {
		return nil, fmt.Errorf("ripgrep not found in PATH")
	}
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []grepMatch{}, nil
		}
		return nil, err
	}
	var matches []grepMatch
	for line := range bytes.SplitSeq(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var match ripgrepMatch
		if err := json.Unmarshal(line, &match); err != nil || match.Type != "match" {
			continue
		}
		for _, submatch := range match.Data.Submatches {
			relative, modTime, err := relativePathAndModTime(rootPath, match.Data.Path.Text)
			if err != nil {
				continue
			}
			matches = append(matches, grepMatch{path: relative, modTime: modTime, lineNum: match.Data.LineNumber, charNum: submatch.Start + 1, lineText: strings.TrimSpace(match.Data.Lines.Text)})
			break
		}
	}
	return matches, nil
}

func searchFilesWithRegex(pattern, rootPath, include string) ([]grepMatch, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	ignorer := NewDirectoryIgnorer(rootPath)
	matches := make([]grepMatch, 0)
	err = filepath.WalkDir(rootPath, func(currentPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		isDir := entry.IsDir()
		if ignorer.ShouldSkip(currentPath, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if isDir {
			return nil
		}
		base := filepath.Base(currentPath)
		if base != "." && strings.HasPrefix(base, ".") {
			return nil
		}
		relative, err := filepath.Rel(rootPath, currentPath)
		if err != nil {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if include != "" {
			matched, err := doublestar.Match(filepath.ToSlash(include), relative)
			if err != nil || !matched {
				return nil
			}
		}
		match, lineNum, charNum, lineText, err := fileContainsPattern(currentPath, regex)
		if err != nil || !match {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		matches = append(matches, grepMatch{path: relative, modTime: info.ModTime(), lineNum: lineNum, charNum: charNum, lineText: lineText})
		if len(matches) >= maxGrepResults*2 {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, err
	}
	return matches, nil
}

func fileContainsPattern(filePath string, pattern *regexp.Regexp) (bool, int, int, string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, 0, 0, "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if !utf8.ValidString(line) {
			return false, 0, 0, "", nil
		}
		if loc := pattern.FindStringIndex(line); loc != nil {
			return true, lineNum, loc[0] + 1, line, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, 0, 0, "", err
	}
	return false, 0, 0, "", nil
}

func relativePathAndModTime(rootPath, matchedPath string) (string, time.Time, error) {
	info, err := os.Stat(matchedPath)
	if err != nil {
		return "", time.Time{}, err
	}
	relative, err := filepath.Rel(rootPath, matchedPath)
	if err != nil {
		return "", time.Time{}, err
	}
	return filepath.ToSlash(relative), info.ModTime(), nil
}
