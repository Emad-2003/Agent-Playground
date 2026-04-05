package permission

import (
	"testing"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
)

func TestCheckToolRejectsDisabledTool(t *testing.T) {
	svc := NewService(config.PermissionsConfig{DisabledTools: []string{"shell"}}, false)
	if !apperrors.IsCode(svc.CheckTool("shell"), apperrors.CodePermissionDenied) {
		t.Fatal("expected disabled tool to be rejected")
	}
}

func TestCheckToolRejectsToolOutsideAllowList(t *testing.T) {
	svc := NewService(config.PermissionsConfig{AllowedTools: []string{"read_file"}}, false)
	if !apperrors.IsCode(svc.CheckTool("grep"), apperrors.CodePermissionDenied) {
		t.Fatal("expected non-whitelisted tool to be rejected")
	}
}

func TestRequiresApprovalHonorsYolo(t *testing.T) {
	svc := NewService(config.PermissionsConfig{}, true)
	if svc.RequiresApproval("shell") {
		t.Fatal("expected yolo mode to skip approval")
	}
}

func TestRequiresApprovalForEditFile(t *testing.T) {
	svc := NewService(config.PermissionsConfig{}, false)
	if !svc.RequiresApproval("edit_file") {
		t.Fatal("expected edit_file to require approval")
	}
}

func TestRequiresApprovalForFetchAndBackgroundShell(t *testing.T) {
	svc := NewService(config.PermissionsConfig{}, false)
	if !svc.RequiresApproval("fetch") {
		t.Fatal("expected fetch to require approval")
	}
	if !svc.RequiresApproval("shell_bg") {
		t.Fatal("expected shell_bg to require approval")
	}
	if svc.RequiresApproval("glob") {
		t.Fatal("expected glob to avoid approval")
	}
	if svc.RequiresApproval("view") {
		t.Fatal("expected view to avoid approval")
	}
	if svc.RequiresApproval("job_output") {
		t.Fatal("expected job_output to avoid approval")
	}
}
