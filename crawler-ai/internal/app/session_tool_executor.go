package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/permission"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/runtime"
	"crawler-ai/internal/session"
	"crawler-ai/internal/shell"
)

type providerToolService interface {
	Definitions() []provider.ToolDefinition
	ExecuteToolCall(ctx context.Context, sessionID string, call provider.ToolCall) (provider.ToolResult, error)
}

type sessionLookup interface {
	Get(id string) (session.Session, bool)
}

type sessionToolExecutor struct {
	runtime     *runtime.Engine
	permissions *permission.Service
	messages    messageStateService
	fileTracker fileTrackingStateService
	sessions    sessionLookup
	nextID      func(string) string
	now         func() time.Time
	status      func(string)
}

func newSessionToolExecutor(runtimeEngine *runtime.Engine, permissions *permission.Service, messages messageStateService, fileTracker fileTrackingStateService, sessions sessionLookup, nextID func(string) string, now func() time.Time, status func(string)) *sessionToolExecutor {
	return &sessionToolExecutor{
		runtime:     runtimeEngine,
		permissions: permissions,
		messages:    messages,
		fileTracker: fileTracker,
		sessions:    sessions,
		nextID:      nextID,
		now:         now,
		status:      status,
	}
}

func (s *sessionToolExecutor) Definitions() []provider.ToolDefinition {
	return append([]provider.ToolDefinition(nil), autonomousToolDefinitions()...)
}

func (s *sessionToolExecutor) ExecuteRequest(ctx context.Context, sessionID string, request runtime.ToolRequest) (runtime.ToolResult, error) {
	result, _, err := s.executeRequest(ctx, sessionID, request, false)
	return result, err
}

func (s *sessionToolExecutor) ExecuteToolCall(ctx context.Context, sessionID string, call provider.ToolCall) (provider.ToolResult, error) {
	request, err := runtimeToolRequestFromCall(call)
	if err != nil {
		if repairedRequest, repairNote, repaired := s.repairToolCall(sessionID, call, err); repaired {
			_, message, execErr := s.executeRequest(ctx, sessionID, repairedRequest, true)
			if execErr != nil {
				if ctx.Err() != nil {
					return provider.ToolResult{}, execErr
				}
				failureContent := providerToolFailureContent(call, execErr)
				return provider.ToolResult{ToolCallID: repairedRequest.CallID, Name: repairedRequest.Name, Content: failureContent, IsError: true}, nil
			}
			if strings.TrimSpace(repairNote) != "" {
				message = repairNote + "\n" + message
			}
			return provider.ToolResult{ToolCallID: repairedRequest.CallID, Name: repairedRequest.Name, Content: message, IsError: false}, nil
		}
		failureContent := providerToolFailureContent(call, err)
		s.persistProviderToolFailure(sessionID, call, failureContent)
		return provider.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: failureContent, IsError: true}, nil
	}
	_, message, execErr := s.executeRequest(ctx, sessionID, request, true)
	if execErr != nil {
		if ctx.Err() != nil {
			return provider.ToolResult{}, execErr
		}
		failureContent := providerToolFailureContent(call, execErr)
		return provider.ToolResult{ToolCallID: request.CallID, Name: request.Name, Content: failureContent, IsError: true}, nil
	}
	return provider.ToolResult{ToolCallID: request.CallID, Name: request.Name, Content: message, IsError: false}, nil
}

func (s *sessionToolExecutor) executeRequest(ctx context.Context, sessionID string, request runtime.ToolRequest, enforceAutonomousPolicy bool) (runtime.ToolResult, string, error) {
	if s == nil || s.runtime == nil || s.messages == nil {
		return runtime.ToolResult{}, "", apperrors.New("app.sessionToolExecutor.executeRequest", apperrors.CodeStartupFailed, "tool executor is not configured")
	}
	request.CallID = ensureToolCallID(request.CallID, s.nextID)
	entry := newRuntimeToolTranscriptEntry(request, s.now())
	if err := s.messages.Append(sessionID, entry); err != nil {
		return runtime.ToolResult{}, "", err
	}
	if err := s.checkRequestPolicy(request, enforceAutonomousPolicy); err != nil {
		entry = applyToolTranscriptResult(entry, err.Error(), toolStatusFailed, s.now())
		_ = s.messages.Update(sessionID, entry)
		return runtime.ToolResult{}, "", err
	}

	result, err := s.runtime.Execute(ctx, request)
	if err != nil {
		if retriedResult, retriedErr, retried := s.retryStaleEditRequest(ctx, sessionID, request, err); retried {
			if retriedErr == nil {
				result = retriedResult
				err = nil
			} else {
				err = retriedErr
			}
		}
	}
	if err != nil {
		entry = applyToolTranscriptResult(entry, err.Error(), toolStatusFailed, s.now())
		_ = s.messages.Update(sessionID, entry)
		return runtime.ToolResult{}, "", err
	}
	message := toolResultMessage(result)
	entry = applyToolTranscriptResult(entry, message, toolStatusCompleted, s.now())
	if strings.TrimSpace(result.Path) != "" {
		entry.Metadata["path"] = result.Path
	}
	for key, value := range result.Extra {
		if entry.Metadata == nil {
			entry.Metadata = make(map[string]string)
		}
		entry.Metadata[key] = value
	}
	if err := s.messages.Update(sessionID, entry); err != nil {
		return runtime.ToolResult{}, "", err
	}
	if s.fileTracker != nil {
		if err := s.fileTracker.TrackToolResult(sessionID, request.Name, result, s.nextID, s.now); err != nil {
			logServiceFailure("session file tracking", err)
		}
	}
	if s.status != nil {
		s.status("Tool completed: " + result.Tool)
	}
	return result, message, nil
}

func (s *sessionToolExecutor) persistProviderToolFailure(sessionID string, call provider.ToolCall, content string) {
	if s == nil || s.messages == nil {
		return
	}
	entry := newProviderToolTranscriptEntry(call, s.now(), nil)
	if appendErr := s.messages.Upsert(sessionID, entry); appendErr != nil {
		logServiceFailure("upsert provider tool failure transcript", appendErr)
		return
	}
	entry = applyToolTranscriptResult(entry, content, toolStatusFailed, s.now())
	logServiceFailure("update provider tool failure transcript", s.messages.Update(sessionID, entry))
}

func providerToolFailureContent(call provider.ToolCall, err error) string {
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		return err.Error()
	}
	return structuredToolRepairHint(call, err)
}

func (s *sessionToolExecutor) repairToolCall(sessionID string, call provider.ToolCall, originalErr error) (runtime.ToolRequest, string, bool) {
	if s == nil || !apperrors.IsCode(originalErr, apperrors.CodeInvalidArgument) {
		return runtime.ToolRequest{}, "", false
	}
	payload, err := decodeToolCallPayload(call.Input)
	if err != nil {
		return runtime.ToolRequest{}, "", false
	}
	request, err := runtimeToolRequestFromPayload(call.ID, strings.TrimSpace(call.Name), payload)
	if err != nil {
		return runtime.ToolRequest{}, "", false
	}
	repairs := make([]string, 0, 2)
	if repairedPath := s.repairMissingPath(sessionID, request, payload); strings.TrimSpace(repairedPath) != "" {
		request.Path = repairedPath
		repairs = append(repairs, fmt.Sprintf("path=%q", repairedPath))
	}
	if s.populateEditOldTextFromWorkspace(sessionID, &request) {
		repairs = append(repairs, "old_text=current file contents")
	}
	if len(repairs) == 0 {
		return runtime.ToolRequest{}, "", false
	}
	if err := validateToolRequest(request); err != nil {
		return runtime.ToolRequest{}, "", false
	}
	return request, fmt.Sprintf("Auto-repaired invalid tool call for %s by filling %s.", request.Name, strings.Join(repairs, ", ")), true
}

func (s *sessionToolExecutor) repairMissingPath(sessionID string, request runtime.ToolRequest, payload map[string]any) string {
	if strings.TrimSpace(request.Path) != "" {
		return request.Path
	}
	switch request.Name {
	case "write_file":
		return inferWriteFilePath(payload, request.Content)
	case "edit_file":
		if strings.TrimSpace(request.OldText) != "" {
			return s.inferExistingWorkspacePath(sessionID, request.OldText)
		}
		return inferWriteFilePath(payload, request.NewText)
	default:
		return ""
	}
}

func (s *sessionToolExecutor) populateEditOldTextFromWorkspace(sessionID string, request *runtime.ToolRequest) bool {
	if request == nil || request.Name != "edit_file" || strings.TrimSpace(request.Path) == "" || strings.TrimSpace(request.OldText) != "" || !looksLikeWholeFileReplacement(request.Path, request.NewText) {
		return false
	}
	currentContent, ok := s.readWorkspaceFile(sessionID, request.Path)
	if !ok {
		return false
	}
	request.OldText = currentContent
	return true
}

func (s *sessionToolExecutor) retryStaleEditRequest(ctx context.Context, sessionID string, request runtime.ToolRequest, originalErr error) (runtime.ToolResult, error, bool) {
	if request.Name != "edit_file" || !strings.Contains(originalErr.Error(), "file content changed since it was last read") || !looksLikeWholeFileReplacement(request.Path, request.NewText) {
		return runtime.ToolResult{}, nil, false
	}
	retryRequest := request
	currentContent, ok := s.readWorkspaceFile(sessionID, retryRequest.Path)
	if !ok || strings.TrimSpace(currentContent) == "" {
		return runtime.ToolResult{}, nil, false
	}
	retryRequest.OldText = currentContent
	result, err := s.runtime.Execute(ctx, retryRequest)
	return result, err, true
}

func inferWriteFilePath(payload map[string]any, content string) string {
	if candidate := stringField(payload, "filename", "name", "target_file", "relative_path"); isSafeRelativePath(candidate) {
		return candidate
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(trimmed, "<!DOCTYPE html"), strings.Contains(lower, "<html"), strings.Contains(lower, "<body"), strings.Contains(lower, "<div"):
		return "index.html"
	case looksLikeCSS(trimmed):
		return "style.css"
	case looksLikeJavaScript(trimmed):
		return "app.js"
	case looksLikeMarkdown(trimmed):
		return "README.md"
	default:
		return ""
	}
}

func looksLikeMarkdown(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.Contains(trimmed, "\n# ")
}

func looksLikeCSS(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.Contains(trimmed, "<") || strings.Contains(trimmed, "function ") {
		return false
	}
	return strings.Contains(trimmed, "{") && strings.Contains(trimmed, "}") && (strings.Contains(trimmed, "@media") || strings.Contains(trimmed, "body {") || strings.Contains(trimmed, "#") || strings.Contains(trimmed, "."))
}

func looksLikeJavaScript(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.Contains(trimmed, "<script") {
		return true
	}
	return strings.Contains(trimmed, "document.") || strings.Contains(trimmed, "addEventListener(") || strings.Contains(trimmed, "const ") || strings.Contains(trimmed, "let ") || strings.Contains(trimmed, "function ")
}

func looksLikeWholeFileReplacement(path, content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		lower := strings.ToLower(trimmed)
		return (strings.HasPrefix(lower, "<!doctype html") || strings.Contains(lower, "<html")) && strings.Contains(lower, "</html>")
	case ".css":
		return len(trimmed) >= 80 && strings.Count(trimmed, "{") >= 2 && strings.Count(trimmed, "}") >= 2
	case ".js", ".mjs", ".cjs":
		return len(trimmed) >= 120 && strings.Count(trimmed, "{") >= 2 && (strings.Contains(trimmed, "function ") || strings.Contains(trimmed, "const ") || strings.Contains(trimmed, "let "))
	case ".md":
		return looksLikeMarkdown(trimmed)
	default:
		return len(trimmed) >= 120 && strings.Count(trimmed, "\n") >= 4
	}
}

func (s *sessionToolExecutor) inferExistingWorkspacePath(sessionID, oldText string) string {
	if s == nil || s.sessions == nil {
		return ""
	}
	sess, ok := s.sessions.Get(sessionID)
	if !ok || strings.TrimSpace(sess.WorkspaceRoot) == "" {
		return ""
	}
	matches := make([]string, 0, 1)
	seen := make(map[string]struct{})
	addMatch := func(path string) bool {
		clean := strings.TrimSpace(filepath.ToSlash(path))
		if clean == "" {
			return false
		}
		if _, exists := seen[clean]; exists {
			return false
		}
		seen[clean] = struct{}{}
		matches = append(matches, clean)
		return len(matches) > 1
	}
	for _, file := range sess.Files {
		if file.Kind != "" && file.Kind != session.FileRecordWorkspace {
			continue
		}
		if !fileContainsText(filepath.Join(sess.WorkspaceRoot, filepath.FromSlash(file.Path)), oldText) {
			continue
		}
		if addMatch(file.Path) {
			return ""
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	limit := 0
	_ = filepath.WalkDir(sess.WorkspaceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".crawler-ai" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if limit >= 200 {
			return filepath.SkipAll
		}
		limit++
		if !fileContainsText(path, oldText) {
			return nil
		}
		rel, relErr := filepath.Rel(sess.WorkspaceRoot, path)
		if relErr != nil {
			return nil
		}
		if addMatch(rel) {
			return filepath.SkipAll
		}
		return nil
	})
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

func (s *sessionToolExecutor) readWorkspaceFile(sessionID, relativePath string) (string, bool) {
	workspaceRoot := s.sessionWorkspaceRoot(sessionID)
	if workspaceRoot == "" || !isSafeRelativePath(relativePath) {
		return "", false
	}
	content, err := os.ReadFile(filepath.Join(workspaceRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		return "", false
	}
	return string(content), true
}

func (s *sessionToolExecutor) sessionWorkspaceRoot(sessionID string) string {
	if s == nil || s.sessions == nil {
		return ""
	}
	sess, ok := s.sessions.Get(sessionID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(sess.WorkspaceRoot)
}

func fileContainsText(path, needle string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(needle) == "" {
		return false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(content) > 512*1024 {
		return false
	}
	return strings.Contains(string(content), needle)
}

func isSafeRelativePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(trimmed))
	return clean != "." && !strings.HasPrefix(clean, "../") && !strings.Contains(clean, ":")
}

func structuredToolRepairHint(call provider.ToolCall, err error) string {
	toolName := strings.TrimSpace(call.Name)
	requiredFields, fieldHints := toolRequirementHints(toolName)
	missingFields := missingFieldsFromValidationError(err.Error())
	receivedInput := parseRepairInput(call.Input)
	payload := map[string]any{
		"status":          "invalid_tool_call",
		"tool":            toolName,
		"error":           err.Error(),
		"required_fields": requiredFields,
		"instruction":     "Resend this tool call with all required fields present. Do not repeat the previous invalid call. Use exactly one corrected tool call.",
	}
	if len(missingFields) > 0 {
		payload["missing_fields"] = missingFields
	}
	if len(fieldHints) > 0 {
		payload["field_hints"] = fieldHints
	}
	if example := toolRepairExample(toolName); len(example) > 0 {
		payload["example"] = example
	}
	if receivedInput != nil {
		payload["received_input"] = receivedInput
	}
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return err.Error()
	}
	return string(raw)
}

func toolRequirementHints(toolName string) ([]string, map[string]string) {
	for _, definition := range autonomousToolDefinitions() {
		if definition.Name != toolName {
			continue
		}
		required := make([]string, 0)
		if rawRequired, ok := definition.Parameters["required"].([]string); ok {
			required = append(required, rawRequired...)
		} else if rawRequired, ok := definition.Parameters["required"].([]any); ok {
			for _, value := range rawRequired {
				if field, ok := value.(string); ok {
					required = append(required, field)
				}
			}
		}
		hints := make(map[string]string)
		properties, _ := definition.Parameters["properties"].(map[string]any)
		for _, field := range required {
			if property, ok := properties[field].(map[string]any); ok {
				if description, ok := property["description"].(string); ok && strings.TrimSpace(description) != "" {
					hints[field] = description
				}
			}
		}
		return required, hints
	}
	return nil, nil
}

func missingFieldsFromValidationError(message string) []string {
	switch {
	case strings.Contains(message, " requires path"):
		return []string{"path"}
	case strings.Contains(message, " requires pattern"):
		return []string{"pattern"}
	case strings.Contains(message, "requires url"):
		return []string{"url"}
	case strings.Contains(message, "requires command"):
		return []string{"command"}
	default:
		return nil
	}
}

func parseRepairInput(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return trimmed
}

func toolRepairExample(toolName string) map[string]string {
	switch toolName {
	case "write_file":
		return map[string]string{"path": "README.md", "content": "file contents"}
	case "edit_file":
		return map[string]string{"path": "index.html", "old_text": "exact current text", "new_text": "replacement text"}
	case "read_file", "view":
		return map[string]string{"path": "README.md"}
	case "glob", "grep":
		return map[string]string{"pattern": "TODO"}
	case "fetch":
		return map[string]string{"url": "https://example.com"}
	case "shell":
		return map[string]string{"command": "pwd"}
	default:
		return nil
	}
}

func (s *sessionToolExecutor) checkRequestPolicy(request runtime.ToolRequest, enforceAutonomousPolicy bool) error {
	if s.permissions == nil {
		return nil
	}
	if err := s.permissions.CheckTool(request.Name); err != nil {
		return err
	}
	if !enforceAutonomousPolicy {
		return nil
	}
	requiresApproval := s.permissions.RequiresApproval(request.Name)
	if requiresApproval && (request.Name == "shell" || request.Name == "shell_bg") && shell.IsSafeReadOnly(request.Command) {
		requiresApproval = false
	}
	if requiresApproval {
		return apperrors.New("app.sessionToolExecutor.checkRequestPolicy", apperrors.CodePermissionDenied, fmt.Sprintf("autonomous tool call requires approval for %s; rerun with --yolo or use explicit commands", request.Name))
	}
	return nil
}

func toolResultMessage(result runtime.ToolResult) string {
	message := result.Output
	if strings.TrimSpace(result.Path) != "" {
		message = result.Path + "\n" + result.Output
	}
	return message
}

func autonomousToolDefinitions() []provider.ToolDefinition {
	return []provider.ToolDefinition{
		{Name: "read_file", Description: "Read a file from the workspace using bounded output.", Parameters: objectSchema(map[string]any{"path": stringSchema("Relative path to the file to read.")}, "path")},
		{Name: "view", Description: "Read a bounded window from a file.", Parameters: objectSchema(map[string]any{"path": stringSchema("Relative path to the file to view."), "start_line": integerSchema("Optional 1-based starting line."), "limit": integerSchema("Optional number of lines to return.")}, "path")},
		{Name: "list_files", Description: "List files in a workspace directory.", Parameters: objectSchema(map[string]any{"path": stringSchema("Optional relative directory path. Leave empty for workspace root.")})},
		{Name: "glob", Description: "Find files by glob pattern relative to the workspace.", Parameters: objectSchema(map[string]any{"pattern": stringSchema("Glob pattern such as **/*.go or *.js.")}, "pattern")},
		{Name: "grep", Description: "Search workspace file contents using a regex pattern.", Parameters: objectSchema(map[string]any{"pattern": stringSchema("Regex pattern to search for.")}, "pattern")},
		{Name: "write_file", Description: "Create a new file. This is create-only and fails if the file already exists.", Parameters: objectSchema(map[string]any{"path": stringSchema("Relative path to create."), "content": stringSchema("Complete file contents.")}, "path", "content")},
		{Name: "edit_file", Description: "Edit an existing file by replacing exact old content with new content.", Parameters: objectSchema(map[string]any{"path": stringSchema("Relative path to edit."), "old_text": stringSchema("Exact current text to replace."), "new_text": stringSchema("Replacement text.")}, "path", "old_text", "new_text")},
		{Name: "fetch", Description: "Fetch and normalize content from an absolute http or https URL.", Parameters: objectSchema(map[string]any{"url": stringSchema("Absolute http or https URL.")}, "url")},
		{Name: "shell", Description: "Run a non-interactive shell command in the workspace.", Parameters: objectSchema(map[string]any{"command": stringSchema("Shell command to execute.")}, "command")},
	}
}

func runtimeToolRequestFromCall(call provider.ToolCall) (runtime.ToolRequest, error) {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return runtime.ToolRequest{}, apperrors.New("app.runtimeToolRequestFromCall", apperrors.CodeInvalidArgument, "tool call name must not be empty")
	}
	payload, err := decodeToolCallPayload(call.Input)
	if err != nil {
		return runtime.ToolRequest{}, err
	}
	request, err := runtimeToolRequestFromPayload(call.ID, name, payload)
	if err != nil {
		return runtime.ToolRequest{}, err
	}
	return request, validateToolRequest(request)
}

func decodeToolCallPayload(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return make(map[string]any), nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, apperrors.Wrap("app.runtimeToolRequestFromCall", apperrors.CodeInvalidArgument, err, "decode tool call input")
	}
	return payload, nil
}

func runtimeToolRequestFromPayload(callID, name string, payload map[string]any) (runtime.ToolRequest, error) {
	request := runtime.ToolRequest{CallID: callID, Name: name}
	switch name {
	case "read_file", "list_files":
		request.Path = stringField(payload, "path", "file_path", "file")
	case "view":
		request.Path = stringField(payload, "path", "file_path", "file")
		request.StartLine = intField(payload, "start_line")
		request.Limit = intField(payload, "limit")
	case "glob", "grep":
		request.Pattern = stringField(payload, "pattern")
	case "write_file":
		request.Path = stringField(payload, "path", "file_path", "file")
		request.Content = normalizeEscapedMultilineContent(stringField(payload, "content"))
	case "edit_file":
		request.Path = stringField(payload, "path", "file_path", "file")
		request.OldText = normalizeEscapedMultilineContent(stringField(payload, "old_text", "old_string"))
		request.NewText = normalizeEscapedMultilineContent(stringField(payload, "new_text", "new_string"))
	case "fetch":
		request.URL = stringField(payload, "url")
	case "shell":
		request.Command = stringField(payload, "command")
	default:
		return runtime.ToolRequest{}, apperrors.New("app.runtimeToolRequestFromCall", apperrors.CodeInvalidArgument, "unsupported autonomous tool: "+name)
	}
	return request, nil
}

func validateToolRequest(request runtime.ToolRequest) error {
	switch request.Name {
	case "list_files":
		return nil
	case "read_file", "view", "write_file", "edit_file":
		if strings.TrimSpace(request.Path) == "" {
			return apperrors.New("app.validateToolRequest", apperrors.CodeInvalidArgument, request.Name+" requires path")
		}
		return nil
	case "glob", "grep":
		if strings.TrimSpace(request.Pattern) == "" {
			return apperrors.New("app.validateToolRequest", apperrors.CodeInvalidArgument, request.Name+" requires pattern")
		}
		return nil
	case "fetch":
		if strings.TrimSpace(request.URL) == "" {
			return apperrors.New("app.validateToolRequest", apperrors.CodeInvalidArgument, "fetch requires url")
		}
		return nil
	case "shell":
		if strings.TrimSpace(request.Command) == "" {
			return apperrors.New("app.validateToolRequest", apperrors.CodeInvalidArgument, "shell requires command")
		}
		return nil
	default:
		return nil
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func stringField(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		stringValue, _ := value.(string)
		if strings.TrimSpace(stringValue) != "" {
			return stringValue
		}
	}
	return ""
}

func normalizeEscapedMultilineContent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(value, "\n") {
		return value
	}
	if !strings.Contains(value, `\n`) && !strings.Contains(value, `\"`) && !strings.Contains(value, `\\`) {
		return value
	}
	normalized := value
	for i := 0; i < 4; i++ {
		candidate := normalized
		candidate = strings.ReplaceAll(candidate, `\\`, `\`)
		candidate = strings.ReplaceAll(candidate, `\n`, "\n")
		candidate = strings.ReplaceAll(candidate, `\t`, "\t")
		candidate = strings.ReplaceAll(candidate, `\r`, "\r")
		candidate = strings.ReplaceAll(candidate, `\"`, `"`)
		candidate = strings.ReplaceAll(candidate, `\'`, `'`)
		if candidate == normalized {
			break
		}
		normalized = candidate
	}
	if looksMeaningfullyNormalized(value, normalized) {
		return normalized
	}
	decoded, err := strconv.Unquote("\"" + strings.ReplaceAll(normalized, "\"", "\\\"") + "\"")
	if err == nil && looksMeaningfullyNormalized(value, decoded) {
		return decoded
	}
	return value
}

func looksMeaningfullyNormalized(original, candidate string) bool {
	if candidate == original {
		return false
	}
	return strings.Count(candidate, "\n") > strings.Count(original, "\n") || strings.Count(candidate, `"`) < strings.Count(original, `"`) || strings.Count(candidate, `\\`) < strings.Count(original, `\\`)
}

func intField(payload map[string]any, key string) int {
	value, ok := payload[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}
