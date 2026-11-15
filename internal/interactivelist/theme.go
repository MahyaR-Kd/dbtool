package interactivelist

import (
	"github.com/MahyaR-Kd/dbtool/internal/ui"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Theme returns the huh theme used by every dbtool prompt, built from the
// ui palette so forms match the rest of the output.
func Theme() *huh.Theme {
	t := huh.ThemeBase()

	accent := ui.ColorAccent
	muted := ui.ColorMuted
	text := ui.ColorText

	sel, unsel, cursor := "✔ ", "○ ", "❯ "
	if ui.ASCII() {
		sel, unsel, cursor = "[x] ", "[ ] ", "> "
	}

	t.Focused.Base = t.Focused.Base.BorderForeground(accent)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(accent).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(accent).Bold(true).MarginBottom(1)
	t.Focused.Description = t.Focused.Description.Foreground(muted)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(ui.ColorError)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(ui.ColorError)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accent).SetString(cursor)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(accent)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(accent)
	t.Focused.Option = t.Focused.Option.Foreground(text)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(accent).SetString(cursor)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(ui.ColorSuccess)
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(ui.ColorSuccess).SetString(sel)
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(muted).SetString(unsel)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(text)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Bold(true)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(text).Background(lipgloss.AdaptiveColor{Light: "252", Dark: "237"})
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(accent)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(muted)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(accent)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	return t
}
