package app

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Submit     key.Binding
	CancelQuit key.Binding
	ClearInput key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Submit:     key.NewBinding(key.WithKeys("ctrl+s")),
		CancelQuit: key.NewBinding(key.WithKeys("ctrl+c")),
		ClearInput: key.NewBinding(key.WithKeys("ctrl+u")),
		PageUp:     key.NewBinding(key.WithKeys("pgup")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown")),
	}
}
