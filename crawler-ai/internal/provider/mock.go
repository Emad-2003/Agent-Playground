package provider

import (
	"context"
	"fmt"
	"strings"
)

type Mock struct {
	model string
}

func NewMock(model string) *Mock {
	return &Mock{model: model}
}

func (m *Mock) Name() string {
	return "mock"
}

func (m *Mock) Complete(_ context.Context, request Request) (Response, error) {
	latest := ""
	if len(request.Messages) > 0 {
		latest = request.Messages[len(request.Messages)-1].Content
	}

	text := fmt.Sprintf("[%s] %s", m.model, strings.TrimSpace(latest))
	if request.SystemPrompt != "" {
		text += " | system: " + strings.TrimSpace(request.SystemPrompt)
	}

	return Response{
		Text:         text,
		Content:      []ContentBlock{TextBlock(text)},
		FinishReason: "stop",
		Usage: Usage{
			InputTokens:  len(strings.Fields(latest)),
			OutputTokens: len(strings.Fields(text)),
		},
	}, nil
}
