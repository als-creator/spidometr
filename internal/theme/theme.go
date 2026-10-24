// Package theme содержит цветовые схемы TUI.
package theme

// Theme — полный набор цветов интерфейса.
type Theme struct {
	ID         string
	Name       string
	Background string
	Foreground string
	Borders    string
	Highlight  string
	HeaderBg   string
	HelpBg     string
	HelpFg     string
	ModalBg    string
	Muted      string
	Download   string
	Upload     string
	Red        string
}

var all = []Theme{
	{
		// Исходная палитра проекта: тёмный slate, indigo, teal и pink.
		ID:         "classic",
		Name:       "Original",
		Background: "#0f172a",
		Foreground: "#e2e8f0",
		Borders:    "#475569",
		Highlight:  "#a5b4fc",
		HeaderBg:   "#1e293b",
		HelpBg:     "#1e293b",
		HelpFg:     "#94a3b8",
		ModalBg:    "#111827",
		Muted:      "#94a3b8",
		Download:   "#2dd4bf",
		Upload:     "#f472b6",
		Red:        "#f87171",
	},
	{
		ID:         "dracula",
		Name:       "Dracula",
		Background: "#282a36",
		Foreground: "#f8f8f2",
		Borders:    "#6272a4",
		Highlight:  "#ff79c6",
		HeaderBg:   "#44475a",
		HelpBg:     "#44475a",
		HelpFg:     "#f8f8f2",
		ModalBg:    "#21222c",
		Muted:      "#6272a4",
		Download:   "#50fa7b",
		Upload:     "#bd93f9",
		Red:        "#ff5555",
	},
	{
		// Палитра OpenCode Ayu (тёмная).
		ID:         "ayu",
		Name:       "Ayu",
		Background: "#0b0e14",
		Foreground: "#bfbdb6",
		Borders:    "#6c7380",
		Highlight:  "#e6b450",
		HeaderBg:   "#0f131a",
		HelpBg:     "#0f131a",
		HelpFg:     "#acb6bf",
		ModalBg:    "#0d1017",
		Muted:      "#565b66",
		Download:   "#aad94c",
		Upload:     "#d2a6ff",
		Red:        "#d95757",
	},
	{
		// Палитра OpenCode Tokyonight (тёмная).
		ID:         "tokyonight",
		Name:       "Tokyonight",
		Background: "#1a1b26",
		Foreground: "#c8d3f5",
		Borders:    "#737aa2",
		Highlight:  "#ff966c",
		HeaderBg:   "#1e2030",
		HelpBg:     "#1e2030",
		HelpFg:     "#c8d3f5",
		ModalBg:    "#222436",
		Muted:      "#828bb8",
		Download:   "#c3e88d",
		Upload:     "#c099ff",
		Red:        "#ff757f",
	},
	{
		ID:         "orange",
		Name:       "Orange",
		Background: "#1a1b25",
		Foreground: "#a3aacb",
		Borders:    "#40445b",
		Highlight:  "#ff9d65",
		HeaderBg:   "#473533",
		HelpBg:     "#322f45",
		HelpFg:     "#9aa3c6",
		ModalBg:    "#282a36",
		Muted:      "#7d8299",
		Download:   "#ff9d65",
		Upload:     "#c9a7ff",
		Red:        "#fe0702",
	},
	{
		ID:         "nord",
		Name:       "Nord",
		Background: "#2e3440",
		Foreground: "#d8dee9",
		Borders:    "#4c566a",
		Highlight:  "#88c0d0",
		HeaderBg:   "#3b4252",
		HelpBg:     "#434c5e",
		HelpFg:     "#d8dee9",
		ModalBg:    "#3b4252",
		Muted:      "#81a1c1",
		Download:   "#8fbcbb",
		Upload:     "#b48ead",
		Red:        "#bf616a",
	},
}

// All возвращает список доступных тем.
func All() []Theme {
	return append([]Theme(nil), all...)
}

// Get находит тему по идентификатору.
func Get(id string) (Theme, bool) {
	for _, t := range all {
		if t.ID == id {
			return t, true
		}
	}
	return Theme{}, false
}

// Default возвращает исходную тему проекта.
func Default() Theme {
	th, _ := Get("classic")
	return th
}
