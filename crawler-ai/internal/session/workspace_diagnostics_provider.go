package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"
)

const (
	defaultWorkspaceDiagnosticsTimeout = 20 * time.Second
	stylelintConfigPayload             = `{"rules":{"block-no-empty":true,"color-no-invalid-hex":true,"property-no-unknown":true,"declaration-block-no-shorthand-property-overrides":true}}`
)

var (
	goplsRangeDiagnosticPattern = regexp.MustCompile(`^(.*):([0-9]+):([0-9]+)(?:-[0-9]+)?:\s*(.*)$`)
	goplsLineDiagnosticPattern  = regexp.MustCompile(`^(.*):([0-9]+):\s*(.*)$`)
	nodeLineDiagnosticPattern   = regexp.MustCompile(`^(.*):([0-9]+)$`)
	nodeSyntaxErrorPattern      = regexp.MustCompile(`^SyntaxError:\s*(.*)$`)
)

type workspaceDiagnosticsRunner func(ctx context.Context, name string, args []string, dir string) ([]byte, error)

type diagnosticsFileSet struct {
	Go         []string
	JavaScript []string
	HTML       []string
	CSS        []string
}

type CommandWorkspaceDiagnosticsProvider struct {
	timeout  time.Duration
	lookPath func(string) (string, error)
	run      workspaceDiagnosticsRunner
}

func NewCommandWorkspaceDiagnosticsProvider() *CommandWorkspaceDiagnosticsProvider {
	return &CommandWorkspaceDiagnosticsProvider{
		timeout: defaultWorkspaceDiagnosticsTimeout,
		lookPath: func(name string) (string, error) {
			return exec.LookPath(name)
		},
		run: func(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec
			cmd.Dir = dir
			return cmd.CombinedOutput()
		},
	}
}

func (p *CommandWorkspaceDiagnosticsProvider) Diagnostics(workspaceRoot string, files []string) (WorkspaceDiagnosticsResult, error) {
	if p == nil {
		return WorkspaceDiagnosticsResult{}, apperrors.New("session.CommandWorkspaceDiagnosticsProvider.Diagnostics", apperrors.CodeStartupFailed, "workspace diagnostics provider is not configured")
	}
	filesByKind := supportedDiagnosticsFiles(files)
	result := WorkspaceDiagnosticsResult{}

	result.Diagnostics = append(result.Diagnostics, p.goDiagnostics(workspaceRoot, filesByKind.Go, &result.Notes)...)
	result.Diagnostics = append(result.Diagnostics, p.javascriptDiagnostics(workspaceRoot, filesByKind.JavaScript, &result.Notes)...)
	result.Diagnostics = append(result.Diagnostics, p.htmlDiagnostics(workspaceRoot, filesByKind.HTML, &result.Notes)...)
	result.Diagnostics = append(result.Diagnostics, p.cssDiagnostics(workspaceRoot, filesByKind.CSS, &result.Notes)...)

	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		if result.Diagnostics[i].Path != result.Diagnostics[j].Path {
			return result.Diagnostics[i].Path < result.Diagnostics[j].Path
		}
		if result.Diagnostics[i].Line != result.Diagnostics[j].Line {
			return result.Diagnostics[i].Line < result.Diagnostics[j].Line
		}
		if result.Diagnostics[i].Column != result.Diagnostics[j].Column {
			return result.Diagnostics[i].Column < result.Diagnostics[j].Column
		}
		return result.Diagnostics[i].Message < result.Diagnostics[j].Message
	})
	result.Notes = deduplicateDiagnosticNotes(result.Notes)
	return result, nil
}

func supportedDiagnosticsFiles(files []string) diagnosticsFileSet {
	seen := make(map[string]struct{}, len(files))
	filtered := diagnosticsFileSet{}
	for _, file := range files {
		trimmed := filepath.ToSlash(filepath.Clean(strings.TrimSpace(file)))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		switch strings.ToLower(filepath.Ext(trimmed)) {
		case ".go":
			filtered.Go = append(filtered.Go, trimmed)
		case ".js", ".mjs", ".cjs":
			filtered.JavaScript = append(filtered.JavaScript, trimmed)
		case ".html", ".htm":
			filtered.HTML = append(filtered.HTML, trimmed)
		case ".css":
			filtered.CSS = append(filtered.CSS, trimmed)
		}
	}
	sort.Strings(filtered.Go)
	sort.Strings(filtered.JavaScript)
	sort.Strings(filtered.HTML)
	sort.Strings(filtered.CSS)
	return filtered
}

func (p *CommandWorkspaceDiagnosticsProvider) goDiagnostics(workspaceRoot string, files []string, notes *[]string) []CodingContextDiagnostic {
	if len(files) == 0 {
		return nil
	}
	if _, err := p.lookPath("gopls"); err != nil {
		appendDiagnosticNote(notes, fmt.Sprintf("go diagnostics unavailable: locate gopls: %v", err))
		return nil
	}
	diagnostics := make([]CodingContextDiagnostic, 0)
	for _, file := range files {
		parsed, ok := p.runCommandDiagnostics(workspaceRoot, "gopls", []string{"check", "-severity=warning", file}, file, parseGoplsDiagnostics)
		diagnostics = append(diagnostics, parsed.Diagnostics...)
		for _, note := range parsed.Notes {
			appendDiagnosticNote(notes, note)
		}
		if ok {
			continue
		}
	}
	return diagnostics
}

func (p *CommandWorkspaceDiagnosticsProvider) javascriptDiagnostics(workspaceRoot string, files []string, notes *[]string) []CodingContextDiagnostic {
	if len(files) == 0 {
		return nil
	}
	if _, err := p.lookPath("node"); err != nil {
		appendDiagnosticNote(notes, fmt.Sprintf("javascript diagnostics unavailable: locate node: %v", err))
		return nil
	}
	diagnostics := make([]CodingContextDiagnostic, 0)
	for _, file := range files {
		parsed, _ := p.runCommandDiagnostics(workspaceRoot, "node", []string{"--check", file}, file, parseNodeCheckDiagnostics)
		diagnostics = append(diagnostics, parsed.Diagnostics...)
		for _, note := range parsed.Notes {
			appendDiagnosticNote(notes, note)
		}
	}
	return diagnostics
}

func (p *CommandWorkspaceDiagnosticsProvider) htmlDiagnostics(workspaceRoot string, files []string, notes *[]string) []CodingContextDiagnostic {
	if len(files) == 0 {
		return nil
	}
	if _, err := p.lookPath("npx"); err != nil {
		appendDiagnosticNote(notes, fmt.Sprintf("html diagnostics unavailable: locate npx: %v", err))
		return nil
	}
	diagnostics := make([]CodingContextDiagnostic, 0)
	for _, file := range files {
		parsed, _ := p.runCommandDiagnostics(workspaceRoot, "npx", []string{"--yes", "htmlhint", "--format", "json", file}, file, func(output []byte, fallbackPath, workspaceRoot string) []CodingContextDiagnostic {
			return parseHTMLHintDiagnostics(output, fallbackPath, workspaceRoot)
		})
		diagnostics = append(diagnostics, parsed.Diagnostics...)
		for _, note := range parsed.Notes {
			appendDiagnosticNote(notes, note)
		}
	}
	return diagnostics
}

func (p *CommandWorkspaceDiagnosticsProvider) cssDiagnostics(workspaceRoot string, files []string, notes *[]string) []CodingContextDiagnostic {
	if len(files) == 0 {
		return nil
	}
	if _, err := p.lookPath("npx"); err != nil {
		appendDiagnosticNote(notes, fmt.Sprintf("css diagnostics unavailable: locate npx: %v", err))
		return nil
	}
	configPath, cleanup, err := writeStylelintConfig()
	if err != nil {
		appendDiagnosticNote(notes, fmt.Sprintf("css diagnostics unavailable: create stylelint config: %v", err))
		return nil
	}
	defer cleanup()

	diagnostics := make([]CodingContextDiagnostic, 0)
	for _, file := range files {
		parsed, _ := p.runCommandDiagnostics(workspaceRoot, "npx", []string{"--yes", "stylelint", "--formatter", "json", "--config", configPath, file}, file, func(output []byte, fallbackPath, workspaceRoot string) []CodingContextDiagnostic {
			return parseStylelintDiagnostics(output, fallbackPath, workspaceRoot)
		})
		diagnostics = append(diagnostics, parsed.Diagnostics...)
		for _, note := range parsed.Notes {
			appendDiagnosticNote(notes, note)
		}
	}
	return diagnostics
}

func (p *CommandWorkspaceDiagnosticsProvider) runCommandDiagnostics(workspaceRoot, name string, args []string, fallbackPath string, parse func([]byte, string, string) []CodingContextDiagnostic) (WorkspaceDiagnosticsResult, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	output, err := p.run(ctx, name, args, workspaceRoot)
	cancel()

	result := WorkspaceDiagnosticsResult{Diagnostics: parse(output, fallbackPath, workspaceRoot)}
	if err == nil || len(result.Diagnostics) > 0 {
		return result, true
	}
	appendDiagnosticNote(&result.Notes, fmt.Sprintf("%s diagnostics unavailable for %s: %v", diagnosticSourceLabel(name, args), fallbackPath, err))
	return result, false
}

func parseGoplsDiagnostics(output []byte, fallbackPath, workspaceRoot string) []CodingContextDiagnostic {
	if len(bytes.TrimSpace(output)) == 0 {
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	items := make([]CodingContextDiagnostic, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if matches := goplsRangeDiagnosticPattern.FindStringSubmatch(line); matches != nil {
			items = append(items, CodingContextDiagnostic{
				Path:     normalizeDiagnosticPath(matches[1], fallbackPath, workspaceRoot),
				Severity: inferDiagnosticSeverity(matches[4]),
				Message:  strings.TrimSpace(matches[4]),
				Source:   "gopls",
				Line:     mustAtoi(matches[2]),
				Column:   mustAtoi(matches[3]),
			})
			continue
		}
		if matches := goplsLineDiagnosticPattern.FindStringSubmatch(line); matches != nil {
			items = append(items, CodingContextDiagnostic{
				Path:     normalizeDiagnosticPath(matches[1], fallbackPath, workspaceRoot),
				Severity: inferDiagnosticSeverity(matches[3]),
				Message:  strings.TrimSpace(matches[3]),
				Source:   "gopls",
				Line:     mustAtoi(matches[2]),
			})
			continue
		}
		items = append(items, CodingContextDiagnostic{
			Path:     filepath.ToSlash(filepath.Clean(strings.TrimSpace(fallbackPath))),
			Severity: inferDiagnosticSeverity(line),
			Message:  line,
			Source:   "gopls",
		})
	}
	return items
}

func parseNodeCheckDiagnostics(output []byte, fallbackPath, workspaceRoot string) []CodingContextDiagnostic {
	if len(bytes.TrimSpace(output)) == 0 {
		return nil
	}
	lines := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		lines = append(lines, strings.TrimRight(scanner.Text(), "\r"))
	}
	lineNumber := 0
	column := 0
	pathValue := strings.TrimSpace(fallbackPath)
	message := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if matches := nodeLineDiagnosticPattern.FindStringSubmatch(trimmed); matches != nil {
			pathValue = matches[1]
			lineNumber = mustAtoi(matches[2])
			if index+2 < len(lines) {
				column = strings.Index(lines[index+2], "^") + 1
			}
			continue
		}
		if matches := nodeSyntaxErrorPattern.FindStringSubmatch(trimmed); matches != nil {
			message = strings.TrimSpace(matches[1])
			break
		}
	}
	if message == "" {
		return nil
	}
	return []CodingContextDiagnostic{{
		Path:     normalizeDiagnosticPath(pathValue, fallbackPath, workspaceRoot),
		Severity: "error",
		Message:  message,
		Source:   "node",
		Line:     lineNumber,
		Column:   column,
	}}
}

func parseHTMLHintDiagnostics(output []byte, fallbackPath, workspaceRoot string) []CodingContextDiagnostic {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var payload []struct {
		File     string `json:"file"`
		Messages []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Line    int    `json:"line"`
			Col     int    `json:"col"`
			Rule    struct {
				ID string `json:"id"`
			} `json:"rule"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil
	}
	items := make([]CodingContextDiagnostic, 0)
	for _, file := range payload {
		for _, message := range file.Messages {
			items = append(items, CodingContextDiagnostic{
				Path:     normalizeDiagnosticPath(file.File, fallbackPath, workspaceRoot),
				Severity: inferDiagnosticSeverity(message.Type + " " + message.Message),
				Message:  strings.TrimSpace(message.Message),
				Source:   "htmlhint",
				Line:     message.Line,
				Column:   message.Col,
			})
		}
	}
	return items
}

func parseStylelintDiagnostics(output []byte, fallbackPath, workspaceRoot string) []CodingContextDiagnostic {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var payload []struct {
		Source      string `json:"source"`
		FilePath    string `json:"filePath"`
		ParseErrors []struct {
			Line   int    `json:"line"`
			Column int    `json:"column"`
			Text   string `json:"text"`
		} `json:"parseErrors"`
		Warnings []struct {
			Line     int    `json:"line"`
			Column   int    `json:"column"`
			Severity string `json:"severity"`
			Text     string `json:"text"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil
	}
	items := make([]CodingContextDiagnostic, 0)
	for _, file := range payload {
		pathValue := file.Source
		if strings.TrimSpace(pathValue) == "" {
			pathValue = file.FilePath
		}
		for _, parseErr := range file.ParseErrors {
			items = append(items, CodingContextDiagnostic{
				Path:     normalizeDiagnosticPath(pathValue, fallbackPath, workspaceRoot),
				Severity: "error",
				Message:  strings.TrimSpace(parseErr.Text),
				Source:   "stylelint",
				Line:     parseErr.Line,
				Column:   parseErr.Column,
			})
		}
		for _, warning := range file.Warnings {
			severity := strings.TrimSpace(warning.Severity)
			if severity == "" {
				severity = inferDiagnosticSeverity(warning.Text)
			}
			items = append(items, CodingContextDiagnostic{
				Path:     normalizeDiagnosticPath(pathValue, fallbackPath, workspaceRoot),
				Severity: severity,
				Message:  strings.TrimSpace(warning.Text),
				Source:   "stylelint",
				Line:     warning.Line,
				Column:   warning.Column,
			})
		}
	}
	return items
}

func normalizeDiagnosticPath(pathValue, fallbackPath, workspaceRoot string) string {
	trimmed := strings.TrimSpace(pathValue)
	if trimmed == "" {
		trimmed = strings.TrimSpace(fallbackPath)
	}
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		if relative, err := filepath.Rel(workspaceRoot, trimmed); err == nil {
			if !strings.HasPrefix(relative, "..") && relative != "." {
				trimmed = relative
			}
		}
	}
	return filepath.ToSlash(filepath.Clean(trimmed))
}

func writeStylelintConfig() (string, func(), error) {
	file, err := os.CreateTemp("", "crawler-ai-stylelint-*.json")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	if _, err := file.WriteString(stylelintConfigPayload); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", func() {}, err
	}
	return path, func() {
		_ = os.Remove(path)
	}, nil
}

func diagnosticSourceLabel(name string, args []string) string {
	switch name {
	case "gopls":
		return "go"
	case "node":
		return "javascript"
	case "npx":
		if len(args) > 1 {
			switch args[1] {
			case "htmlhint":
				return "html"
			case "stylelint":
				return "css"
			}
		}
	}
	return name
}

func appendDiagnosticNote(notes *[]string, note string) {
	if notes == nil {
		return
	}
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return
	}
	*notes = append(*notes, trimmed)
}

func deduplicateDiagnosticNotes(notes []string) []string {
	if len(notes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(notes))
	filtered := make([]string, 0, len(notes))
	for _, note := range notes {
		trimmed := strings.TrimSpace(note)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		filtered = append(filtered, trimmed)
	}
	sort.Strings(filtered)
	return filtered
}

func inferDiagnosticSeverity(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(lower, "warning") || strings.Contains(lower, "deprecated") {
		return "warning"
	}
	if strings.Contains(lower, "hint") || strings.Contains(lower, "consider") {
		return "hint"
	}
	return "error"
}

func mustAtoi(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}
