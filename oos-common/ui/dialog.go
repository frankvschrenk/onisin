package ui

// dialog.go — Theme-aware variants of Fyne's confirm dialogs.
//
// Fyne's dialog.ShowConfirm renders both buttons in the same importance,
// so a destructive action ("Really delete?") looks identical to a benign
// one. The helpers in this file paint the confirm button with the theme's
// semantic importance, which feeds through to the Warning / Error colour
// slot of the active OOS theme:
//
//   ShowWarningConfirm  → primary button in Warning colour (amber)
//                         for destructive confirmations
//   ShowDangerConfirm   → primary button in Error colour (red)
//                         for hard, irreversible destructive actions
//
// Everything else — modal behaviour, focus, keyboard handling — matches
// dialog.ShowConfirm, so the helpers are a drop-in replacement.

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ShowWarningConfirm renders a confirm dialog whose confirm button uses
// widget.WarningImportance. The title, message and callback semantics
// match dialog.ShowConfirm. Empty confirm/dismiss strings fall back to
// German defaults ("Ja" / "Nein").
func ShowWarningConfirm(title, message, confirm, dismiss string, callback func(bool), parent fyne.Window) {
	showThemedConfirm(title, message, confirm, dismiss, widget.WarningImportance, callback, parent)
}

// ShowDangerConfirm renders a confirm dialog whose confirm button uses
// widget.DangerImportance. Use for irreversible deletions or teardown
// operations where the user must really intend to proceed.
func ShowDangerConfirm(title, message, confirm, dismiss string, callback func(bool), parent fyne.Window) {
	showThemedConfirm(title, message, confirm, dismiss, widget.DangerImportance, callback, parent)
}

// showThemedConfirm builds a CustomConfirm dialog so we can set the
// confirm button's importance via SetConfirmImportance.
func showThemedConfirm(title, message, confirm, dismiss string, importance widget.Importance, callback func(bool), parent fyne.Window) {
	if confirm == "" {
		confirm = "Ja"
	}
	if dismiss == "" {
		dismiss = "Nein"
	}

	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord

	d := dialog.NewCustomConfirm(title, confirm, dismiss, label, callback, parent)
	d.SetConfirmImportance(importance)
	d.Show()
}
