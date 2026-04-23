// icons.go — DSL style-Hints → Fyne Theme-Icons.
//
// Zwei Funktionen:
//   - buttonIcon(style)     — für <button style="...">
//   - themeIconByName(name) — für <icon name="...">
//
// Icon-Namen orientieren sich an https://docs.fyne.io/explore/icons/
package dsl

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// buttonIcon gibt das passende Theme-Icon für einen Button-Style zurück.
func buttonIcon(style string) fyne.Resource {
	switch style {
	case "save", "primary":
		return theme.DocumentSaveIcon()
	case "delete", "danger":
		return theme.DeleteIcon()
	case "cancel", "secondary":
		return theme.CancelIcon()
	case "add":
		return theme.ContentAddIcon()
	case "refresh":
		return theme.ViewRefreshIcon()
	case "home":
		return theme.HomeIcon()
	case "settings":
		return theme.SettingsIcon()
	case "search":
		return theme.SearchIcon()
	case "info":
		return theme.InfoIcon()
	case "edit":
		return theme.DocumentCreateIcon()
	case "copy":
		return theme.ContentCopyIcon()
	default:
		return theme.ConfirmIcon()
	}
}

// themeIconByName gibt das Fyne Theme-Icon für einen Icon-Namen zurück.
// Namen orientieren sich an https://docs.fyne.io/explore/icons/
// Fallback: theme.QuestionIcon() wenn der Name unbekannt ist.
func themeIconByName(name string) fyne.Resource {
	switch name {
	// Navigation
	case "home":
		return theme.HomeIcon()
	case "settings", "gear":
		return theme.SettingsIcon()
	case "search":
		return theme.SearchIcon()
	case "menu":
		return theme.MenuIcon()
	case "back":
		return theme.NavigateBackIcon()
	case "forward":
		return theme.NavigateNextIcon()

	// Aktionen
	case "add", "plus":
		return theme.ContentAddIcon()
	case "remove", "minus":
		return theme.ContentRemoveIcon()
	case "delete", "trash":
		return theme.DeleteIcon()
	case "edit", "create":
		return theme.DocumentCreateIcon()
	case "save":
		return theme.DocumentSaveIcon()
	case "copy":
		return theme.ContentCopyIcon()
	case "cut":
		return theme.ContentCutIcon()
	case "paste":
		return theme.ContentPasteIcon()
	case "undo":
		return theme.ContentUndoIcon()
	case "redo":
		return theme.ContentRedoIcon()
	case "refresh":
		return theme.ViewRefreshIcon()
	case "cancel", "close":
		return theme.CancelIcon()
	case "confirm", "check", "ok":
		return theme.ConfirmIcon()

	// Status & Info
	case "info":
		return theme.InfoIcon()
	case "warning":
		return theme.WarningIcon()
	case "error":
		return theme.ErrorIcon()
	case "help", "question":
		return theme.HelpIcon()

	// Medien
	case "play":
		return theme.MediaPlayIcon()
	case "pause":
		return theme.MediaPauseIcon()
	case "stop":
		return theme.MediaStopIcon()
	case "skip-forward":
		return theme.MediaSkipNextIcon()
	case "skip-back":
		return theme.MediaSkipPreviousIcon()
	case "fast-forward":
		return theme.MediaFastForwardIcon()

	// Dokumente & Dateien
	case "document":
		return theme.DocumentIcon()
	case "folder":
		return theme.FolderIcon()
	case "folder-open":
		return theme.FolderOpenIcon()
	case "download":
		return theme.DownloadIcon()
	case "upload":
		return theme.UploadIcon()

	// Ansicht
	case "zoom-in":
		return theme.ZoomInIcon()
	case "zoom-out":
		return theme.ZoomOutIcon()
	case "zoom-fit":
		return theme.ZoomFitIcon()
	case "list":
		return theme.ListIcon()
	case "grid":
		return theme.GridIcon()

	// Sonstiges
	case "account", "user":
		return theme.AccountIcon()
	case "color":
		return theme.ColorPaletteIcon()
	case "visibility", "eye":
		return theme.VisibilityIcon()
	case "visibility-off":
		return theme.VisibilityOffIcon()
	case "mail":
		return theme.MailComposeIcon()
	case "computer":
		return theme.ComputerIcon()
	case "storage":
		return theme.StorageIcon()
	case "logout":
		return theme.LogoutIcon()
	case "login":
		return theme.LoginIcon()

	default:
		return theme.QuestionIcon()
	}
}
