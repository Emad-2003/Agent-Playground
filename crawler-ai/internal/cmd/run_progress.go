package cmd

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"crawler-ai/internal/app"
)

type runProgressRenderer struct {
	w          io.Writer
	mu         sync.Mutex
	lastStatus string
	lastTask   string
	lastBusy   *bool
	lastUsage  string
	reasoning  map[string]string
}

func newRunProgressRenderer(w io.Writer) *runProgressRenderer {
	return &runProgressRenderer{w: w, reasoning: make(map[string]string)}
}

func (r *runProgressRenderer) Handle(event app.NonInteractiveProgressEvent) {
	if r == nil || r.w == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	switch event.Kind {
	case app.NonInteractiveProgressStatus:
		message := normalizeProgressText(event.Message)
		if message == "" || message == r.lastStatus {
			return
		}
		r.lastStatus = message
		r.print(event.Timestamp, "status", message)
	case app.NonInteractiveProgressReasoning:
		message := normalizeProgressText(event.Message)
		if message == "" {
			return
		}
		previous := r.reasoning[event.ID]
		delta := strings.TrimSpace(strings.TrimPrefix(message, previous))
		if delta == "" && message == previous {
			return
		}
		if delta == "" {
			delta = message
		}
		r.reasoning[event.ID] = message
		r.print(event.Timestamp, "think", normalizeProgressText(delta))
	case app.NonInteractiveProgressTask:
		message := normalizeProgressText(event.Message)
		if message == "" || message == r.lastTask {
			return
		}
		r.lastTask = message
		r.print(event.Timestamp, "plan", message)
	case app.NonInteractiveProgressToolStarted:
		r.print(event.Timestamp, "tool", strings.TrimSpace("start "+formatToolProgressDetail(event)))
	case app.NonInteractiveProgressToolCompleted:
		r.print(event.Timestamp, "tool", strings.TrimSpace("done  "+formatToolProgressDetail(event)))
	case app.NonInteractiveProgressToolFailed:
		r.print(event.Timestamp, "tool", strings.TrimSpace("fail  "+formatToolProgressDetail(event)))
	case app.NonInteractiveProgressApprovalRequired:
		r.print(event.Timestamp, "approval", normalizeProgressText(event.Message))
	case app.NonInteractiveProgressBusy:
		if r.lastBusy != nil && *r.lastBusy == event.Busy {
			return
		}
		busy := event.Busy
		r.lastBusy = &busy
		message := "idle"
		if event.Busy {
			message = "working"
		}
		r.print(event.Timestamp, "state", message)
	case app.NonInteractiveProgressTokenUsage:
		message := formatTokenUsageProgress(event)
		if message == "" || message == r.lastUsage {
			return
		}
		r.lastUsage = message
		r.print(event.Timestamp, "usage", message)
	}
}

func (r *runProgressRenderer) print(timestamp string, label string, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	stamp := "--:--:--"
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(timestamp)); err == nil {
		stamp = parsed.Local().Format("15:04:05")
	}
	_, _ = fmt.Fprintf(r.w, "[%s] %-8s %s\n", stamp, label, message)
}

func formatToolProgressDetail(event app.NonInteractiveProgressEvent) string {
	parts := make([]string, 0, 3)
	if tool := strings.TrimSpace(event.Tool); tool != "" {
		parts = append(parts, tool)
	}
	if path := strings.TrimSpace(event.Path); path != "" {
		parts = append(parts, path)
	}
	if detail := normalizeProgressText(event.Detail); detail != "" {
		parts = append(parts, detail)
	}
	return strings.Join(parts, " ")
}

func formatTokenUsageProgress(event app.NonInteractiveProgressEvent) string {
	if event.InputTokens == 0 && event.OutputTokens == 0 && event.TotalCost == 0 {
		return ""
	}
	parts := []string{}
	if event.InputTokens > 0 || event.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in=%s out=%s", shortTokenCount(event.InputTokens), shortTokenCount(event.OutputTokens)))
	}
	if event.TotalCost > 0 {
		parts = append(parts, fmt.Sprintf("cost=$%.4f", event.TotalCost))
	}
	return strings.Join(parts, " ")
}

func shortTokenCount(value int64) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func normalizeProgressText(value string) string {
	text := strings.Join(strings.Fields(strings.ReplaceAll(value, "\r\n", "\n")), " ")
	if len(text) > 160 {
		return text[:157] + "..."
	}
	return text
}
