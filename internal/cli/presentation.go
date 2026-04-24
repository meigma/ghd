package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"
)

type presentationMode struct {
	nonInteractive bool
	dynamic        bool
	color          bool
	enhanced       bool
	statusLine     bool
}

type uiStyles struct {
	accent lipgloss.Style
	muted  lipgloss.Style
	label  lipgloss.Style
	title  lipgloss.Style
}

type uiRow struct {
	label string
	value string
}

func detectPresentationMode(options Options) presentationMode {
	nonInteractive := options.Viper.GetBool("non-interactive")
	inputTTY := readerIsTerminal(options.In)
	errTTY := writerIsTerminal(options.Err)
	dynamic := !nonInteractive && inputTTY && errTTY
	color := dynamic && colorEnabled()
	return presentationMode{
		nonInteractive: nonInteractive,
		dynamic:        dynamic,
		color:          color,
		enhanced:       dynamic,
		statusLine:     dynamic,
	}
}

func readerIsTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func writerIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

func newUIStyles(color bool) uiStyles {
	if !color {
		return uiStyles{
			accent: lipgloss.NewStyle(),
			muted:  lipgloss.NewStyle(),
			label:  lipgloss.NewStyle(),
			title:  lipgloss.NewStyle(),
		}
	}
	return uiStyles{
		accent: lipgloss.NewStyle().Foreground(lipgloss.Color("#2fb7a0")),
		muted:  lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")),
		label:  lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")),
		title:  lipgloss.NewStyle().Foreground(lipgloss.Color("#2fb7a0")).Bold(true),
	}
}

type statusLine struct {
	w      io.Writer
	styles uiStyles
	frame  int
	active bool
}

func newStatusLine(w io.Writer, color bool) *statusLine {
	return &statusLine{
		w:      w,
		styles: newUIStyles(color),
	}
}

func (s *statusLine) Update(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	s.UpdateLine(fmt.Sprintf("%s %s", s.nextFrame(), message))
}

func (s *statusLine) UpdateLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	fmt.Fprintf(s.w, "\r\033[K%s", line)
	s.active = true
}

func (s *statusLine) Clear() {
	if !s.active {
		return
	}
	fmt.Fprint(s.w, "\r\033[K")
	s.active = false
}

func (s *statusLine) nextFrame() string {
	frames := [...]string{"-", "\\", "|", "/"}
	frame := s.styles.accent.Render(frames[s.frame%len(frames)])
	s.frame++
	return frame
}

func runPromptForm(ctx context.Context, options Options, mode presentationMode, groups ...*huh.Group) error {
	form := huh.NewForm(groups...).
		WithInput(options.In).
		WithOutput(options.Err).
		WithTheme(huhTheme(mode.color)).
		WithShowHelp(false)
	if mode.dynamic {
		form = form.WithViewHook(func(view tea.View) tea.View {
			view.AltScreen = true
			return view
		})
	}
	return form.RunWithContext(ctx)
}

func huhTheme(color bool) huh.Theme {
	if color {
		return huh.ThemeFunc(huh.ThemeCharm)
	}
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		styles := huh.ThemeBase(isDark)
		button := lipgloss.NewStyle().Padding(0, 2).MarginRight(1)
		styles.Focused.FocusedButton = button
		styles.Focused.BlurredButton = button
		styles.Blurred.FocusedButton = button
		styles.Blurred.BlurredButton = button
		return styles
	})
}

func formatRows(rows []uiRow, labelWidth int) string {
	var b strings.Builder
	for _, row := range rows {
		if strings.TrimSpace(row.value) == "" {
			continue
		}
		fmt.Fprintf(&b, "%-*s %s\n", labelWidth, row.label+":", terminalSafeText(row.value))
	}
	return strings.TrimRight(b.String(), "\n")
}

func escapeNoteDescription(value string) string {
	var b strings.Builder
	for _, char := range terminalSafeText(value) {
		switch char {
		case '\\', '_', '*', '`':
			b.WriteRune('\\')
		}
		b.WriteRune(char)
	}
	return b.String()
}

func terminalSafeText(value string) string {
	var b strings.Builder
	for _, char := range value {
		switch char {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\x1b':
			b.WriteString(`\x1b`)
		default:
			if unicode.IsControl(char) {
				if char <= 0xff {
					fmt.Fprintf(&b, "\\x%02x", char)
					continue
				}
				fmt.Fprintf(&b, "\\u%04x", char)
				continue
			}
			b.WriteRune(char)
		}
	}
	return b.String()
}

func terminalSafeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, terminalSafeText(value))
	}
	return out
}

func writeTrustedRootNotice(w io.Writer, path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	fmt.Fprintf(w, "using custom Sigstore trust root %s\n", terminalSafeText(path))
}

func renderProgressBar(ratio float64, width int, styles uiStyles) string {
	if width <= 0 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	fill := strings.Repeat("#", filled)
	empty := strings.Repeat("-", width-filled)
	return "[" + styles.accent.Render(fill) + styles.muted.Render(empty) + "]"
}

func formatByteCount(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value = value / 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/1024)
}
