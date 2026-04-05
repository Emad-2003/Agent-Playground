package styles

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func getColorRGB(color lipgloss.TerminalColor) (uint8, uint8, uint8) {
	r, g, b, a := color.RGBA()
	if a > 0 && a < 0xffff {
		r = (r * 0xffff) / a
		g = (g * 0xffff) / a
		b = (b * 0xffff) / a
	}
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

func ForceReplaceBackgroundWithLipgloss(input string, newBackground lipgloss.TerminalColor) string {
	r, g, b := getColorRGB(newBackground)
	replacement := fmt.Sprintf("48;2;%d;%d;%d", r, g, b)

	return ansiEscape.ReplaceAllStringFunc(input, func(sequence string) string {
		const prefixLength = 2
		const suffixLength = 1
		start := prefixLength
		end := len(sequence) - suffixLength

		var builder strings.Builder
		builder.Grow((end - start) + len(replacement) + 2)

		for index := start; index < end; {
			next := index
			for next < end && sequence[next] != ';' {
				next++
			}
			token := sequence[index:next]

			if len(token) == 2 && token[0] == '4' && token[1] == '8' {
				candidate := next + 1
				if candidate < end {
					candidateEnd := candidate
					for candidateEnd < end && sequence[candidateEnd] != ';' {
						candidateEnd++
					}
					mode := sequence[candidate:candidateEnd]
					if mode == "5" {
						skip := candidateEnd + 1
						for skip < end && sequence[skip] != ';' {
							skip++
						}
						index = skip + 1
						continue
					}
					if mode == "2" {
						skip := candidateEnd + 1
						for channel := 0; channel < 3 && skip < end; channel++ {
							for skip < end && sequence[skip] != ';' {
								skip++
							}
							skip++
						}
						index = skip
						continue
					}
				}
			}

			keep := true
			value := 0
			for position := index; position < next; position++ {
				char := sequence[position]
				if char < '0' || char > '9' {
					value = -1
					break
				}
				value = value*10 + int(char-'0')
			}
			if value >= 0 && ((value >= 40 && value <= 47) || (value >= 100 && value <= 107) || value == 49) {
				keep = false
			}

			if keep {
				if builder.Len() > 0 {
					builder.WriteByte(';')
				}
				builder.WriteString(token)
			}
			index = next + 1
		}

		if builder.Len() > 0 {
			builder.WriteByte(';')
		}
		builder.WriteString(replacement)
		return "\x1b[" + builder.String() + "m"
	})
}
