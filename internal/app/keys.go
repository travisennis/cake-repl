package app

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Submit           key.Binding
	CancelQuit       key.Binding
	NewSession       key.Binding
	ClearInput       key.Binding
	PageUp           key.Binding
	PageDown         key.Binding
	HistoryPrev      key.Binding
	HistoryNext      key.Binding
	TabComplete      key.Binding
	ToggleToolOutput key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Submit:           key.NewBinding(key.WithKeys("ctrl+s")),
		CancelQuit:       key.NewBinding(key.WithKeys("ctrl+c")),
		NewSession:       key.NewBinding(key.WithKeys("ctrl+n")),
		ClearInput:       key.NewBinding(key.WithKeys("ctrl+u")),
		PageUp:           key.NewBinding(key.WithKeys("pgup")),
		PageDown:         key.NewBinding(key.WithKeys("pgdown")),
		HistoryPrev:      key.NewBinding(key.WithKeys("up")),
		HistoryNext:      key.NewBinding(key.WithKeys("down")),
		TabComplete:      key.NewBinding(key.WithKeys("tab")),
		ToggleToolOutput: key.NewBinding(key.WithKeys("ctrl+o")),
	}
}
