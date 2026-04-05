package permission

import (
	"strings"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
)

type Service struct {
	yolo     bool
	allowed  map[string]struct{}
	disabled map[string]struct{}
}

func NewService(cfg config.PermissionsConfig, yolo bool) *Service {
	service := &Service{
		yolo:     yolo,
		allowed:  make(map[string]struct{}),
		disabled: make(map[string]struct{}),
	}
	for _, tool := range cfg.AllowedTools {
		tool = normalize(tool)
		if tool != "" {
			service.allowed[tool] = struct{}{}
		}
	}
	for _, tool := range cfg.DisabledTools {
		tool = normalize(tool)
		if tool != "" {
			service.disabled[tool] = struct{}{}
		}
	}
	return service
}

func (s *Service) CheckTool(toolName string) error {
	name := normalize(toolName)
	if name == "" {
		return apperrors.New("permission.CheckTool", apperrors.CodeInvalidArgument, "tool name must not be empty")
	}
	if _, blocked := s.disabled[name]; blocked {
		return apperrors.New("permission.CheckTool", apperrors.CodePermissionDenied, "tool disabled by configuration: "+name)
	}
	if len(s.allowed) > 0 {
		if _, ok := s.allowed[name]; !ok {
			return apperrors.New("permission.CheckTool", apperrors.CodePermissionDenied, "tool not allowed by configuration: "+name)
		}
	}
	return nil
}

func (s *Service) RequiresApproval(toolName string) bool {
	if s.yolo {
		return false
	}
	switch normalize(toolName) {
	case "write_file", "edit_file", "fetch", "shell", "shell_bg":
		return true
	default:
		return false
	}
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
