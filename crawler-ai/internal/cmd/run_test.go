package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crawler-ai/internal/app"
	"crawler-ai/internal/domain"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/session"
)

func TestRunCommandPrintsAssistantReplyAndPersistsSession(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "hello world")
	if err != nil {
		t.Fatalf("run command error: %v", err)
	}
	if !strings.Contains(stdout, "[mock-orchestrator-v1] hello world") {
		t.Fatalf("expected mock provider output, got %q", stdout)
	}

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	runSessions := persistedRunSessions(mgr.List())
	if len(runSessions) != 1 {
		t.Fatalf("expected one persisted non-interactive session, got %#v", mgr.List())
	}
	if len(runSessions[0].Transcript) < 2 {
		t.Fatalf("expected user and assistant transcript entries, got %#v", runSessions[0].Transcript)
	}
	if runSessions[0].Transcript[0].Kind != domain.TranscriptUser || runSessions[0].Transcript[1].Kind != domain.TranscriptAssistant {
		t.Fatalf("expected user then assistant transcript entries, got %#v", runSessions[0].Transcript)
	}
}

func TestRunCommandContinueUsesExistingSession(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	if _, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "hello"); err != nil {
		t.Fatalf("initial run command error: %v", err)
	}

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	initial := persistedRunSessions(mgr.List())
	if len(initial) != 1 {
		t.Fatalf("expected one initial session, got %#v", mgr.List())
	}
	initialID := initial[0].ID

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "--continue", "follow up")
	if err != nil {
		t.Fatalf("continue run command error: %v", err)
	}
	if !strings.Contains(stdout, "[mock-orchestrator-v1] follow up") {
		t.Fatalf("expected follow-up output, got %q", stdout)
	}

	mgr = session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	continued, ok := mgr.Get(initialID)
	if !ok {
		t.Fatalf("expected continued session %s", initialID)
	}
	if len(persistedRunSessions(mgr.List())) != 1 {
		t.Fatalf("expected continue to reuse the existing session, got %#v", mgr.List())
	}
	if len(continued.Transcript) < 4 {
		t.Fatalf("expected transcript to grow in the same session, got %#v", continued.Transcript)
	}
}

func TestRunCommandUsesExplicitSessionID(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	if _, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "hello"); err != nil {
		t.Fatalf("initial run command error: %v", err)
	}

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	created := persistedRunSessions(mgr.List())
	if len(created) != 1 {
		t.Fatalf("expected one session after initial run, got %#v", mgr.List())
	}

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "--session", created[0].ID, "again")
	if err != nil {
		t.Fatalf("explicit session run error: %v", err)
	}
	if !strings.Contains(stdout, "[mock-orchestrator-v1] again") {
		t.Fatalf("expected explicit-session output, got %q", stdout)
	}

	mgr = session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(persistedRunSessions(mgr.List())) != 1 {
		t.Fatalf("expected explicit session run to reuse the existing session, got %#v", mgr.List())
	}
}

func TestRunCommandTextModePrintsProgressToStderr(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	if err := os.WriteFile(filepath.Join(workspaceDir, "notes.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	stdout, stderr, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "/read notes.txt")
	if err != nil {
		t.Fatalf("run command error: %v", err)
	}
	if !strings.Contains(stdout, "hello") {
		t.Fatalf("expected file content in stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "tool") || !strings.Contains(stderr, "read_file") {
		t.Fatalf("expected live progress in stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, "status") {
		t.Fatalf("expected status progress in stderr, got %q", stderr)
	}
}

func TestReadRunPromptCombinesArgsAndStdin(t *testing.T) {
	prompt, err := readRunPrompt(bytes.NewBufferString("from stdin"), []string{"hello"})
	if err != nil {
		t.Fatalf("readRunPrompt() error: %v", err)
	}
	if prompt != "hello\n\nfrom stdin" {
		t.Fatalf("expected combined prompt, got %q", prompt)
	}
}

func TestRunCommandDataDirOverridePersistsSessionUnderOverride(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	dataDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	stdout, _, err := executeRootCommandForTest(t, "--data-dir", dataDir, "--cwd", workspaceDir, "run", "hello override")
	if err != nil {
		t.Fatalf("run command error: %v", err)
	}
	if !strings.Contains(stdout, "[mock-orchestrator-v1] hello override") {
		t.Fatalf("expected mock provider output, got %q", stdout)
	}

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(dataDir, "sessions"))
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(persistedRunSessions(mgr.List())) != 1 {
		t.Fatalf("expected one persisted session under override, got %#v", mgr.List())
	}

	defaultDirEntries, err := os.ReadDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	if err == nil && len(defaultDirEntries) > 0 {
		t.Fatalf("expected default profile session directory to remain unused, got %d entries", len(defaultDirEntries))
	}
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir(default sessions) error: %v", err)
	}
}

func TestRunCommandJSONFormatIncludesSessionAndRenderedOutput(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "--format", "json", "hello json")
	if err != nil {
		t.Fatalf("run command error: %v", err)
	}

	var result app.NonInteractiveRunResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput: %s", err, stdout)
	}
	if result.SessionID == "" {
		t.Fatal("expected session id in json output")
	}
	if result.Status != app.NonInteractiveRunStatusOK {
		t.Fatalf("expected ok status in json output, got %#v", result)
	}
	if result.ExitCode != app.NonInteractiveRunExitOK {
		t.Fatalf("expected zero exit code in json output, got %#v", result)
	}
	if !strings.Contains(result.RenderedOutput, "[mock-orchestrator-v1] hello json") {
		t.Fatalf("expected rendered output in json payload, got %#v", result)
	}
	if result.Session.ID != result.SessionID {
		t.Fatalf("expected session payload to match session id, got %#v", result)
	}
	if len(result.Entries) == 0 {
		t.Fatalf("expected visible transcript entries, got %#v", result)
	}
	if len(result.Session.Transcript) < 2 {
		t.Fatalf("expected persisted transcript in session payload, got %#v", result.Session)
	}
}

func TestRunCommandJSONFormatApprovalRequiredIncludesStructuredFailure(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "--format", "json", "/write notes.txt::secret body")
	if err == nil {
		t.Fatal("expected approval-required run to return an error")
	}
	var exitErr *commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected commandExitError, got %T", err)
	}
	if exitErr.exitCode != app.NonInteractiveRunExitApprovalRequired {
		t.Fatalf("expected approval exit code %d, got %d", app.NonInteractiveRunExitApprovalRequired, exitErr.exitCode)
	}

	var result app.NonInteractiveRunResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal approval json output: %v\noutput: %s", err, stdout)
	}
	if result.Status != app.NonInteractiveRunStatusApprovalRequired {
		t.Fatalf("expected approval_required status, got %#v", result)
	}
	if result.ExitCode != app.NonInteractiveRunExitApprovalRequired {
		t.Fatalf("expected approval exit code in payload, got %#v", result)
	}
	if result.Failure == nil || result.Failure.ApprovalAction != "write_file" {
		t.Fatalf("expected approval action in payload, got %#v", result)
	}
	if result.Failure.Code != "permission_denied" {
		t.Fatalf("expected permission_denied code, got %#v", result)
	}
	if result.SessionID == "" {
		t.Fatalf("expected approval result to include session id, got %#v", result)
	}
	if result.Session.ID != result.SessionID {
		t.Fatalf("expected session payload to match session id, got %#v", result)
	}
}

func TestRunCommandJSONFormatProviderFailureIncludesStructuredFailure(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"type":"server_error","message":"upstream unavailable"}}`))
	}))
	defer server.Close()

	t.Setenv("CRAWLER_AI_ORCHESTRATOR_PROVIDER", "openai")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_MODEL", "gpt-test")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_BASE_URL", server.URL)
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_API_KEY", "test-key")

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "--format", "json", "hello provider")
	if err == nil {
		t.Fatal("expected provider-failed run to return an error")
	}
	var exitErr *commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected commandExitError, got %T", err)
	}
	if exitErr.exitCode != app.NonInteractiveRunExitProviderFailed {
		t.Fatalf("expected provider exit code %d, got %d", app.NonInteractiveRunExitProviderFailed, exitErr.exitCode)
	}

	var result app.NonInteractiveRunResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal provider-failure json output: %v\noutput: %s", err, stdout)
	}
	if result.Status != app.NonInteractiveRunStatusProviderFailed {
		t.Fatalf("expected provider_failed status, got %#v", result)
	}
	if result.ExitCode != app.NonInteractiveRunExitProviderFailed {
		t.Fatalf("expected provider exit code in payload, got %#v", result)
	}
	if result.Failure == nil || result.Failure.ProviderStatus == nil {
		t.Fatalf("expected provider status details, got %#v", result)
	}
	if result.Failure.Code != "provider_failed" {
		t.Fatalf("expected provider_failed code, got %#v", result)
	}
	if result.Failure.ProviderStatus.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected provider status code %d, got %#v", http.StatusBadGateway, result)
	}
	if !strings.Contains(result.Failure.ProviderStatus.Body, "upstream unavailable") {
		t.Fatalf("expected response body detail in payload, got %#v", result)
	}
	if result.SessionID == "" {
		t.Fatalf("expected provider failure result to include session id, got %#v", result)
	}
}

func TestRunCommandAutonomousFlagBypassesApproval(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "run", "-a", "/write notes.txt::autonomous body")
	if err != nil {
		t.Fatalf("autonomous run command error: %v", err)
	}
	if !strings.Contains(stdout, "notes.txt") || !strings.Contains(stdout, "wrote 15 bytes") {
		t.Fatalf("expected write output, got %q", stdout)
	}

	data, err := os.ReadFile(filepath.Join(workspaceDir, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != "autonomous body" {
		t.Fatalf("expected autonomous file content, got %q", string(data))
	}
}

func persistedRunSessions(sessions []session.Session) []session.Session {
	filtered := make([]session.Session, 0, len(sessions))
	for _, sess := range sessions {
		if len(sess.Transcript) == 0 {
			continue
		}
		filtered = append(filtered, sess)
	}
	return filtered
}
