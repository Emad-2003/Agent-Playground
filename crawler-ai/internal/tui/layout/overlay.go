package layout

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/termenv"

	muesliAnsi "github.com/muesli/ansi"
)

// PlaceOverlay places fg on top of bg at position (x, y), with an optional
// shadow effect behind the overlay. This enables centered dialog rendering
// over the current page content.
func PlaceOverlay(x, y int, fg, bg string, shadow bool, opts ...WhitespaceOption) string {
	fgLines, fgWidth, fgHeight := getLines(fg)
	bgLines, bgWidth, bgHeight := getLines(bg)

	if fgWidth >= bgWidth && fgHeight >= bgHeight {
		return fg
	}

	x = clamp(x, 0, bgWidth-fgWidth)
	y = clamp(y, 0, bgHeight-fgHeight)

	ws := &whitespace{}
	for _, opt := range opts {
		opt(ws)
	}

	var b strings.Builder
	for i, bgLine := range bgLines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if i < y || i >= y+fgHeight {
			b.WriteString(bgLine)
			continue
		}

		fgLine := fgLines[i-y]
		fgLineWidth := muesliAnsi.PrintableRuneWidth(fgLine)

		if x > 0 {
			left := truncate.String(bgLine, uint(x))
			leftWidth := muesliAnsi.PrintableRuneWidth(left)
			if leftWidth < x {
				left += ws.render(x - leftWidth)
			}
			b.WriteString(left)
		}

		b.WriteString(fgLine)

		rightStart := x + fgLineWidth
		if rightStart < bgWidth {
			bgLineWidth := muesliAnsi.PrintableRuneWidth(bgLine)
			if bgLineWidth > rightStart {
				right := ansi.Cut(bgLine, rightStart, bgLineWidth)
				b.WriteString(right)
			} else {
				b.WriteString(ws.render(bgWidth - rightStart))
			}
		}
	}

	if shadow {
		return applyShadow(b.String(), x, y, fgWidth, fgHeight, bgWidth, bgHeight)
	}

	return b.String()
}

func applyShadow(s string, x, y, fgW, fgH, bgW, bgH int) string {
	lines := strings.Split(s, "\n")
	shadowChar := "░"

	// Right shadow
	for i := y + 1; i < y+fgH+1 && i < len(lines); i++ {
		col := x + fgW
		if col < bgW {
			lineW := muesliAnsi.PrintableRuneWidth(lines[i])
			if col < lineW {
				left := truncate.String(lines[i], uint(col))
				rest := ansi.Cut(lines[i], col, lineW)
				// Replace first char of rest with shadow
				if muesliAnsi.PrintableRuneWidth(rest) > 0 {
					restCut := ansi.Cut(rest, 1, muesliAnsi.PrintableRuneWidth(rest))
					lines[i] = left + shadowChar + restCut
				}
			}
		}
	}

	// Bottom shadow
	bottomY := y + fgH
	if bottomY < len(lines) {
		lineW := muesliAnsi.PrintableRuneWidth(lines[bottomY])
		startX := x + 1
		endX := x + fgW + 1
		if endX > lineW {
			endX = lineW
		}
		if startX < endX {
			left := truncate.String(lines[bottomY], uint(startX))
			right := ""
			if endX < lineW {
				right = ansi.Cut(lines[bottomY], endX, lineW)
			}
			shadowStr := strings.Repeat(shadowChar, endX-startX)
			lines[bottomY] = left + shadowStr + right
		}
	}

	return strings.Join(lines, "\n")
}

func getLines(s string) ([]string, int, int) {
	lines := strings.Split(s, "\n")
	maxWidth := 0
	for _, line := range lines {
		w := muesliAnsi.PrintableRuneWidth(line)
		if w > maxWidth {
			maxWidth = w
		}
	}
	return lines, maxWidth, len(lines)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// WhitespaceOption configures whitespace rendering.
type WhitespaceOption func(*whitespace)

type whitespace struct {
	style termenv.Style
	chars string
}

func (ws *whitespace) render(width int) string {
	if width <= 0 {
		return ""
	}
	c := ws.chars
	if c == "" {
		c = " "
	}
	return strings.Repeat(c, width)
}
