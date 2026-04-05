package chat

import (
	"regexp"
	"strings"
	"testing"

	"crawler-ai/internal/domain"
)

var listANSISequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestTranscriptRenderBlocksGroupsAssistantSegmentsByResponseID(t *testing.T) {
	entries := []domain.TranscriptEntry{
		{ID: "user-1", Kind: domain.TranscriptUser, Message: "hello"},
		{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "answer", Metadata: map[string]string{domain.TranscriptMetadataResponseID: "assistant-1"}},
		{ID: "reasoning-1", Kind: domain.TranscriptReasoning, Message: "thinking", Metadata: map[string]string{domain.TranscriptMetadataResponseID: "assistant-1"}},
		{ID: "tool-1", Kind: domain.TranscriptTool, Message: "tool output", Metadata: map[string]string{domain.TranscriptMetadataResponseID: "assistant-1", "tool": "grep"}},
		{ID: "system-1", Kind: domain.TranscriptSystem, Message: "done"},
	}

	blocks := transcriptRenderBlocks(entries)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 render blocks, got %d", len(blocks))
	}
	if len(blocks[1].entries) != 3 {
		t.Fatalf("expected grouped assistant block with 3 entries, got %#v", blocks[1].entries)
	}
	if blocks[1].entries[0].Kind != domain.TranscriptAssistant || blocks[1].entries[1].Kind != domain.TranscriptReasoning || blocks[1].entries[2].Kind != domain.TranscriptTool {
		t.Fatalf("unexpected grouped block ordering: %#v", blocks[1].entries)
	}
}

func TestRenderToolSegmentShowsStructuredSections(t *testing.T) {
	entry := domain.TranscriptEntry{
		Kind:     domain.TranscriptTool,
		Message:  "Tool: grep\n\nInput:\n{\"pattern\":\"TODO\",\"path\":\"README.md\"}\n\nResult:\nREADME.md:1: TODO",
		Metadata: map[string]string{"tool": "grep", "status": "completed"},
	}

	rendered := renderToolSegment(entry, 60)
	for _, expected := range []string{"INPUT", "RESULT", "COMPLETED", "pattern=TODO", "path=README.md", "README.md:1: TODO"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered tool segment to contain %q, got %q", expected, rendered)
		}
	}
}

func TestRenderTranscriptBlockUsesMarkdownAwareAssistantRendering(t *testing.T) {
	block := transcriptRenderBlock{entries: []domain.TranscriptEntry{
		{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "# Title\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\n```go\nfmt.Println(\"hi\")\n```", Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1"}},
		{ID: "tool-1", Kind: domain.TranscriptTool, Message: "Tool: grep\n\nInput:\nTODO\n\nResult:\nREADME.md:1: TODO", Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1", "tool": "grep", "status": "completed"}},
	}}

	rendered := renderTranscriptBlock(block, 80)
	plain := listANSISequence.ReplaceAllString(rendered, "")
	for _, expected := range []string{"Title", "A", "B", "fmt.Println(\"hi\")", "README.md:1: TODO"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("expected grouped render to contain %q, got %q", expected, plain)
		}
	}
	if strings.Contains(plain, "```go") {
		t.Fatalf("expected markdown syntax to be rendered inside grouped block, got %q", plain)
	}
}
