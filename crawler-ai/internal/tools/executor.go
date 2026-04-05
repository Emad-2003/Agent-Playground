package tools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/fsext"
	"crawler-ai/internal/shell"
)

type Result struct {
	Output string
	Path   string
	Extra  map[string]string
}

type Executor struct {
	workspaceRoot string
	httpClient    *http.Client
}

func NewExecutor(workspaceRoot string) (*Executor, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, apperrors.New("tools.NewExecutor", apperrors.CodeInvalidArgument, "workspace root must not be empty")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second

	return &Executor{
		workspaceRoot: filepath.Clean(workspaceRoot),
		httpClient:    &http.Client{Timeout: defaultFetchTimeout, Transport: transport},
	}, nil
}

func (e *Executor) ReadFile(path string) (Result, error) {
	return e.View(path, 1, defaultViewLineLimit)
}

func (e *Executor) WriteFile(path, content string) (Result, error) {
	relative, err := fsext.WriteFile(e.workspaceRoot, path, []byte(content))
	if err != nil {
		return Result{}, err
	}

	return Result{Output: fmt.Sprintf("wrote %d bytes", len(content)), Path: relative}, nil
}

func (e *Executor) EditFile(path, expectedOldContent, newContent string) (Result, error) {
	relative, historyRelative, err := fsext.EditFile(e.workspaceRoot, path, []byte(expectedOldContent), []byte(newContent))
	if err != nil {
		return Result{}, err
	}
	return Result{
		Output: fmt.Sprintf("updated %s and saved snapshot to %s", relative, historyRelative),
		Path:   relative,
		Extra:  map[string]string{"history_path": historyRelative},
	}, nil
}

func (e *Executor) ListFiles(path string) (Result, error) {
	resolved, _, err := fsext.ResolveWithinWorkspace(e.workspaceRoot, defaultPath(path))
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

func (e *Executor) RunShell(ctx context.Context, command string) (Result, error) {
	output, err := shell.Run(ctx, e.workspaceRoot, command)
	result := Result{Output: output}
	if err != nil {
		return result, err
	}

	return result, nil
}

func (e *Executor) RunBackgroundShell(ctx context.Context, command string) (Result, error) {
	job, err := shell.GetBackgroundJobManager().Start(ctx, e.workspaceRoot, command)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.RunBackgroundShell", apperrors.CodeToolFailed, err, "start background shell")
	}
	return Result{Output: fmt.Sprintf("started background shell job %s", job.ID), Extra: map[string]string{"job_id": job.ID}}, nil
}

func (e *Executor) GetBackgroundShellOutput(jobID string) (Result, error) {
	job, ok := shell.GetBackgroundJobManager().Get(strings.TrimSpace(jobID))
	if !ok {
		return Result{}, apperrors.New("tools.GetBackgroundShellOutput", apperrors.CodeInvalidArgument, "background job not found")
	}
	stdout, stderr, done, err := job.GetOutput()
	status := "running"
	if done {
		status = "completed"
	}
	output := []string{fmt.Sprintf("Background job %s (%s)", job.ID, status)}
	if strings.TrimSpace(stdout) != "" {
		output = append(output, "STDOUT:\n"+stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		output = append(output, "STDERR:\n"+stderr)
	}
	if err != nil {
		output = append(output, "EXIT ERROR:\n"+err.Error())
	}
	return Result{Output: strings.Join(output, "\n\n"), Extra: map[string]string{"job_id": job.ID, "status": status}}, nil
}

func (e *Executor) KillBackgroundShell(jobID string) (Result, error) {
	if err := shell.GetBackgroundJobManager().Kill(strings.TrimSpace(jobID)); err != nil {
		return Result{}, apperrors.New("tools.KillBackgroundShell", apperrors.CodeInvalidArgument, err.Error())
	}
	return Result{Output: fmt.Sprintf("background shell job %s terminated", strings.TrimSpace(jobID)), Extra: map[string]string{"job_id": strings.TrimSpace(jobID)}}, nil
}

func defaultPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return path
}
