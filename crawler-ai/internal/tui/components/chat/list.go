package chat

import (
	"fmt"
	"sort"
	"strings"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MessageKeys for scrolling the message list.
type MessageKeys struct {
	PageDown     key.Binding
	PageUp       key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
}

var messageKeys = MessageKeys{
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdn", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "½ page up"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "½ page down"),
	),
}

type messagesCmp struct {
	width, height int
	viewport      viewport.Model
	transcript    []domain.TranscriptEntry
	spinner       spinner.Model
	busy          bool
	streaming     bool
}

func (m *messagesCmp) Init() tea.Cmd {
	return tea.Batch(m.viewport.Init(), m.spinner.Tick)
}

func (m *messagesCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case AddTranscriptMsg:
		m.transcript = append(m.transcript, msg.Entry)
		m.renderView()
		m.viewport.GotoBottom()
		return m, nil

	case UpdateTranscriptMsg:
		for index := range m.transcript {
			if m.transcript[index].ID == msg.Entry.ID {
				m.transcript[index] = msg.Entry
				m.renderView()
				m.viewport.GotoBottom()
				return m, nil
			}
		}
		m.transcript = append(m.transcript, msg.Entry)
		m.renderView()
		m.viewport.GotoBottom()
		return m, nil

	case SetTranscriptMsg:
		m.transcript = append([]domain.TranscriptEntry(nil), msg.Entries...)
		m.renderView()
		m.viewport.GotoBottom()
		return m, nil

	case StreamDeltaMsg:
		if len(m.transcript) > 0 {
			last := &m.transcript[len(m.transcript)-1]
			if last.Kind == domain.TranscriptAssistant {
				last.Message += msg.Text
				m.streaming = true
				m.renderView()
				m.viewport.GotoBottom()
				return m, nil
			}
		}
		m.transcript = append(m.transcript, domain.TranscriptEntry{
			Kind:    domain.TranscriptAssistant,
			Message: msg.Text,
		})
		m.streaming = true
		m.renderView()
		m.viewport.GotoBottom()
		return m, nil

	case StreamDoneMsg:
		m.streaming = false
		return m, nil

	case SetBusyMsg:
		m.busy = bool(msg)
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
		}
	}

	s, cmd := m.spinner.Update(msg)
	m.spinner = s
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *messagesCmp) renderView() {
	if m.width == 0 {
		return
	}
	baseStyle := styles.BaseStyle()

	var messages []string
	for _, block := range transcriptRenderBlocks(m.transcript) {
		rendered := renderTranscriptBlock(block, m.width)
		messages = append(messages, rendered, baseStyle.Width(m.width).Render(""))
	}

	m.viewport.SetContent(
		baseStyle.Width(m.width).Render(
			lipgloss.JoinVertical(lipgloss.Top, messages...),
		),
	)
}

type transcriptRenderBlock struct {
	entries []domain.TranscriptEntry
}

func transcriptRenderBlocks(entries []domain.TranscriptEntry) []transcriptRenderBlock {
	blocks := make([]transcriptRenderBlock, 0, len(entries))
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		responseID := transcriptResponseID(entry)
		if responseID == "" || !isAssistantSegment(entry.Kind) {
			blocks = append(blocks, transcriptRenderBlock{entries: []domain.TranscriptEntry{entry}})
			continue
		}

		group := []domain.TranscriptEntry{entry}
		for next := index + 1; next < len(entries); next++ {
			candidate := entries[next]
			if transcriptResponseID(candidate) != responseID || !isAssistantSegment(candidate.Kind) {
				break
			}
			group = append(group, candidate)
			index = next
		}
		blocks = append(blocks, transcriptRenderBlock{entries: group})
	}
	return blocks
}

func transcriptResponseID(entry domain.TranscriptEntry) string {
	if entry.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(entry.Metadata[domain.TranscriptMetadataResponseID])
}

func isAssistantSegment(kind domain.TranscriptKind) bool {
	switch kind {
	case domain.TranscriptAssistant, domain.TranscriptReasoning, domain.TranscriptTool:
		return true
	default:
		return false
	}
}

func renderTranscriptBlock(block transcriptRenderBlock, width int) string {
	if len(block.entries) == 1 {
		return renderTranscriptEntry(block.entries[0], width)
	}
	return renderAssistantResponseGroup(block.entries, width)
}

func renderTranscriptEntry(entry domain.TranscriptEntry, width int) string {
	t := theme.CurrentTheme()

	var chipStyle lipgloss.Style
	var chipText string
	accent := t.Primary()

	switch entry.Kind {
	case domain.TranscriptUser:
		chipStyle = lipgloss.NewStyle().
			Background(t.Primary()).
			Foreground(t.BackgroundDarker()).
			Bold(true).
			Padding(0, 1)
		chipText = "YOU"
		accent = t.Secondary()
	case domain.TranscriptAssistant:
		chipStyle = lipgloss.NewStyle().
			Background(t.Accent()).
			Foreground(t.BackgroundDarker()).
			Bold(true).
			Padding(0, 1)
		chipText = "AI"
		accent = t.Primary()
	case domain.TranscriptReasoning:
		chipStyle = lipgloss.NewStyle().
			Background(t.TextMuted()).
			Foreground(t.BackgroundDarker()).
			Bold(true).
			Padding(0, 1)
		chipText = "THINK"
		accent = t.TextMuted()
	case domain.TranscriptTool:
		chipStyle = lipgloss.NewStyle().
			Background(t.Secondary()).
			Foreground(t.BackgroundDarker()).
			Bold(true).
			Padding(0, 1)
		chipText = "TOOL"
		accent = t.Secondary()
	case domain.TranscriptSystem:
		chipStyle = lipgloss.NewStyle().
			Background(t.TextMuted()).
			Foreground(t.BackgroundDarker()).
			Padding(0, 1)
		chipText = "SYS"
		accent = t.TextMuted()
	}

	chip := chipStyle.Render(chipText)

	var timePart string
	if !entry.CreatedAt.IsZero() {
		timePart = lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Render(" " + entry.CreatedAt.Format("15:04"))
	}

	header := chip + timePart

	// Metadata badges
	if badges := renderMetadataBadges(entry.Metadata); badges != "" {
		header += "  " + badges
	}

	msgWidth := max(10, width-2)
	body := renderFramedMessageItem(entry.Message, accent, msgWidth)
	if entry.Kind == domain.TranscriptUser || entry.Kind == domain.TranscriptAssistant || entry.Kind == domain.TranscriptSystem {
		body = renderMarkdownFramedMessageItem(entry.Message, accent, msgWidth)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func renderAssistantResponseGroup(entries []domain.TranscriptEntry, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	first := entries[0]

	header := lipgloss.NewStyle().
		Background(t.Accent()).
		Foreground(t.BackgroundDarker()).
		Bold(true).
		Padding(0, 1).
		Render("AI")
	if !first.CreatedAt.IsZero() {
		header += lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Render(" " + first.CreatedAt.Format("15:04"))
	}
	if badges := renderMetadataBadges(first.Metadata); badges != "" {
		header += "  " + badges
	}

	segments := make([]string, 0, len(entries))
	for _, entry := range entries {
		if rendered := renderAssistantSegment(entry, width-2); rendered != "" {
			segments = append(segments, rendered)
		}
	}

	body := baseStyle.
		Width(max(10, width-2)).
		PaddingLeft(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, segments...))

	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func renderAssistantSegment(entry domain.TranscriptEntry, width int) string {
	switch entry.Kind {
	case domain.TranscriptAssistant:
		if strings.TrimSpace(entry.Message) == "" {
			return ""
		}
		return renderMarkdownFramedMessageItem(entry.Message, theme.CurrentTheme().Primary(), max(10, width))
	case domain.TranscriptReasoning:
		return renderReasoningSegment(entry.Message, width)
	case domain.TranscriptTool:
		return renderToolSegment(entry, width)
	default:
		return renderTranscriptEntry(entry, width)
	}
}

func renderReasoningSegment(message string, width int) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return ""
	}
	paragraphs := splitSegmentParagraphs(trimmed)
	styled := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		styled = append(styled, lipgloss.NewStyle().Faint(true).Render(paragraph))
	}
	return renderLabeledSegment("THINK", theme.CurrentTheme().TextMuted(), strings.Join(styled, "\n\n"), width, "")
}

func renderToolSegment(entry domain.TranscriptEntry, width int) string {
	t := theme.CurrentTheme()
	toolName := "TOOL"
	status := ""
	if entry.Metadata != nil {
		if name := strings.TrimSpace(entry.Metadata["tool"]); name != "" {
			toolName = strings.ToUpper(name)
		}
		status = strings.TrimSpace(entry.Metadata["status"])
	}
	parsed := parseToolTranscriptMessage(entry.Message)
	if parsed.name != "" {
		toolName = strings.ToUpper(parsed.name)
	}

	sections := make([]string, 0, 2)
	if parsed.input != "" {
		inputBody := parsed.input
		if summary := summarizeToolPayload(parsed.input, width-8); summary != "" && summary != strings.TrimSpace(parsed.input) {
			inputBody = summary + "\n" + parsed.input
		}
		sections = append(sections, renderSegmentSection("INPUT", inputBody, width-2, t.TextSecondary()))
	}
	if parsed.result != "" {
		resultColor := t.Success()
		if status == "failed" {
			resultColor = t.Error()
		}
		sections = append(sections, renderSegmentSection("RESULT", renderToolResultBody(toolName, parsed.result, status, width-2), width-2, resultColor))
	}
	if len(sections) == 0 && strings.TrimSpace(entry.Message) != "" {
		sections = append(sections, styles.BaseStyle().Width(max(8, width-2)).Render(strings.TrimSpace(entry.Message)))
	}

	return renderLabeledSegment(toolName, t.Secondary(), strings.Join(sections, "\n"), width, status)
}

func renderSegmentSection(label string, body string, width int, accent lipgloss.AdaptiveColor) string {
	sectionLabel := lipgloss.NewStyle().
		Foreground(accent).
		Bold(true).
		Render(label)
	sectionBody := renderFramedMessageItem(strings.TrimSpace(body), accent, max(8, width))
	return lipgloss.JoinVertical(lipgloss.Left, sectionLabel, sectionBody)
}

func splitSegmentParagraphs(message string) []string {
	parts := strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n\n")
	paragraphs := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}
	if len(paragraphs) == 0 && strings.TrimSpace(message) != "" {
		return []string{strings.TrimSpace(message)}
	}
	return paragraphs
}

type toolTranscriptMessage struct {
	name   string
	input  string
	result string
}

func parseToolTranscriptMessage(message string) toolTranscriptMessage {
	parsed := toolTranscriptMessage{}
	for _, part := range strings.Split(message, "\n\n") {
		switch {
		case strings.HasPrefix(part, "Tool: "):
			parsed.name = strings.TrimSpace(strings.TrimPrefix(part, "Tool: "))
		case strings.HasPrefix(part, "Input:\n"):
			parsed.input = strings.TrimSpace(strings.TrimPrefix(part, "Input:\n"))
		case strings.HasPrefix(part, "Result:\n"):
			parsed.result = strings.TrimSpace(strings.TrimPrefix(part, "Result:\n"))
		}
	}
	return parsed
}

func renderLabeledSegment(label string, accent lipgloss.AdaptiveColor, message string, width int, status string) string {
	t := theme.CurrentTheme()
	segmentStyle := lipgloss.NewStyle().
		BorderLeft(true).
		BorderForeground(accent).
		PaddingLeft(1).
		Width(max(10, width))
	badge := lipgloss.NewStyle().
		Background(accent).
		Foreground(t.BackgroundDarker()).
		Bold(true).
		Padding(0, 1).
		Render(label)
	headerParts := []string{badge}
	if status != "" {
		headerParts = append(headerParts, renderSegmentStatusBadge(status))
	}
	return segmentStyle.Render(lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, headerParts...),
		styles.BaseStyle().Width(max(8, width-2)).Render(message),
	))
}

func renderSegmentStatusBadge(status string) string {
	t := theme.CurrentTheme()
	trimmed := strings.ToLower(strings.TrimSpace(status))
	background := t.TextMuted()
	switch trimmed {
	case "completed":
		background = t.Success()
	case "failed":
		background = t.Error()
	case "running", "in_progress":
		background = t.Accent()
	case "pending":
		background = t.Secondary()
	}
	return lipgloss.NewStyle().
		Background(background).
		Foreground(t.BackgroundDarker()).
		Bold(true).
		Padding(0, 1).
		Render(strings.ToUpper(trimmed))
}

func renderMetadataBadges(metadata map[string]string) string {
	if len(metadata) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	hidden := map[string]struct{}{
		domain.TranscriptMetadataResponseID: {},
	}
	keys := make([]string, 0, len(metadata))
	for key, value := range metadata {
		if _, skip := hidden[key]; skip || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	badges := make([]string, 0, len(keys))
	for _, key := range keys {
		badges = append(badges, lipgloss.NewStyle().Foreground(t.TextMuted()).Render(fmt.Sprintf("%s:%s", key, metadata[key])))
	}
	return strings.Join(badges, " ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *messagesCmp) View() string {
	baseStyle := styles.BaseStyle()

	if len(m.transcript) == 0 {
		return baseStyle.
			Width(m.width).
			Height(m.height).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					m.initialScreen(),
					"",
					m.help(),
				),
			)
	}

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				m.viewport.View(),
				m.working(),
				m.help(),
			),
		)
}

func (m *messagesCmp) initialScreen() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	logo := fmt.Sprintf("%s %s", styles.CrawlerIcon, "crawler-ai")
	logoRendered := baseStyle.
		Bold(true).
		Width(m.width).
		Render(logo)

	cwd := baseStyle.
		Foreground(t.TextMuted()).
		Width(m.width).
		Render("Ready. Type a message below to start.")

	return baseStyle.Width(m.width).Height(m.height - 2).Render(
		lipgloss.JoinVertical(lipgloss.Top, logoRendered, "", cwd),
	)
}

func (m *messagesCmp) working() string {
	if !m.busy || len(m.transcript) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	task := "Thinking..."
	if m.streaming {
		task = "Generating..."
	}
	return baseStyle.
		Width(m.width).
		Foreground(t.Primary()).
		Bold(true).
		Render(fmt.Sprintf("%s %s ", m.spinner.View(), task))
}

func (m *messagesCmp) help() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if m.busy {
		return lipgloss.JoinHorizontal(
			lipgloss.Left,
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render("press "),
			baseStyle.Foreground(t.Text()).Bold(true).Render("esc"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to cancel"),
		)
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		baseStyle.Foreground(t.TextMuted()).Bold(true).Render("press "),
		baseStyle.Foreground(t.Text()).Bold(true).Render("enter"),
		baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to send, write "),
		baseStyle.Foreground(t.Text()).Bold(true).Render("\\"),
		baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" + enter for newline"),
	)
}

func (m *messagesCmp) SetSize(width, height int) tea.Cmd {
	if m.width == width && m.height == height {
		return nil
	}
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = height - 2
	m.renderView()
	return nil
}

func (m *messagesCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *messagesCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		messageKeys.PageUp,
		messageKeys.PageDown,
		messageKeys.HalfPageUp,
		messageKeys.HalfPageDown,
	}
}

func newMessagesCmp() *messagesCmp {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	vp := viewport.New(0, 0)
	vp.KeyMap.PageUp = messageKeys.PageUp
	vp.KeyMap.PageDown = messageKeys.PageDown
	vp.KeyMap.HalfPageUp = messageKeys.HalfPageUp
	vp.KeyMap.HalfPageDown = messageKeys.HalfPageDown
	return &messagesCmp{
		viewport: vp,
		spinner:  s,
	}
}
