package helper

// setup_dialog.go — First-run and settings dialog (Fyne).
//
// ShowSetupDialog is called when no oos.toml exists yet.
// OpenSettingsDialog is called from the main menu at any time.

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos-common/llm"
)

// NativeInputResult carries the values collected by the setup or settings dialog.
type NativeInputResult struct {
	OOSPUrl      string
	DexURL       string
	VaultURL     string
	ClientID     string
	LLMAddr      string
	LLMApiKey    string
	LLMChatModel string
	Aborted      bool
}

// ShowSetupDialog opens the first-run configuration dialog and blocks until
// the user saves or cancels.
func ShowSetupDialog(current SetupConfig) NativeInputResult {
	return showSetupDialog(current, "")
}

// ShowErrorThenSetup opens the setup dialog with an error message pre-filled.
func ShowErrorThenSetup(current SetupConfig, errorMsg string) NativeInputResult {
	return showSetupDialog(current, errorMsg)
}

// ShowErrorDialog displays a blocking error dialog with an OK button.
func ShowErrorDialog(title, message string) {
	a := app.New()
	w := a.NewWindow(title)
	w.Resize(fyne.NewSize(400, 150))
	w.CenterOnScreen()

	lbl := widget.NewLabel(message)
	lbl.Wrapping = fyne.TextWrapWord
	btn := widget.NewButton("OK", func() { a.Quit() })

	w.SetContent(container.NewVBox(lbl, btn))
	w.ShowAndRun()
}

func showSetupDialog(current SetupConfig, errorMsg string) NativeInputResult {
	a := app.New()
	w := a.NewWindow("OOS Settings")
	w.Resize(fyne.NewSize(520, 580))
	w.CenterOnScreen()
	w.SetFixedSize(true)

	errLabel := canvas.NewText("", color.RGBA{R: 200, G: 50, B: 50, A: 255})
	errLabel.TextStyle = fyne.TextStyle{Bold: true}
	errLabel.Alignment = fyne.TextAlignCenter
	if errorMsg != "" {
		errLabel.Text = errorMsg
		go func() {
			time.Sleep(5 * time.Second)
			errLabel.Text = ""
			errLabel.Refresh()
		}()
	}

	oospEntry := widget.NewEntry()
	oospEntry.SetPlaceHolder("http://localhost:9100")
	oospEntry.SetText(orDefault(current.OOSPUrl, "http://localhost:9100"))

	dexEntry := widget.NewEntry()
	dexEntry.SetPlaceHolder("http://localhost:5556")
	dexEntry.SetText(orDefault(current.DexURL, "http://localhost:5556"))

	clientIDEntry := widget.NewEntry()
	clientIDEntry.SetPlaceHolder("oos-desktop")
	clientIDEntry.SetText(orDefault(current.ClientID, "oos-desktop"))

	vaultEntry := widget.NewEntry()
	vaultEntry.SetPlaceHolder("http://localhost:8200")
	vaultEntry.SetText(orDefault(current.VaultURL, "http://localhost:8200"))

	llmAddrEntry := widget.NewEntry()
	llmAddrEntry.SetPlaceHolder("http://localhost:11434")
	llmAddrEntry.SetText(orDefault(current.LLMAddr, llm.URL))

	llmApiKeyEntry := widget.NewPasswordEntry()
	llmApiKeyEntry.SetPlaceHolder("leave empty for local models")
	llmApiKeyEntry.SetText(current.LLMApiKey)

	llmModelEntry := widget.NewEntry()
	llmModelEntry.SetPlaceHolder("e.g. gemma4:26b")
	llmModelEntry.SetText(orDefault(current.LLMChatModel, llm.ChatModel))

	pingBtn := widget.NewButton("Test OOSP", func() {
		ping := PingOOSP(oospEntry.Text)
		if ping.OOSP != "ok" {
			errLabel.Color = color.RGBA{R: 200, G: 50, B: 50, A: 255}
			errLabel.Text = "OOSP: " + ping.OOSP
		} else {
			errLabel.Color = color.RGBA{R: 50, G: 150, B: 50, A: 255}
			errLabel.Text = "OOSP reachable"
		}
		errLabel.Refresh()
	})

	result := NativeInputResult{Aborted: true}
	done := make(chan struct{})

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "OOSP URL", Widget: oospEntry, HintText: "Plugin server"},
			{Text: "Dex URL", Widget: dexEntry, HintText: "OIDC issuer"},
			{Text: "Client ID", Widget: clientIDEntry, HintText: "OIDC client ID"},
			{Text: "Vault URL", Widget: vaultEntry, HintText: "OpenBao / Vault"},
			{Text: "LLM URL", Widget: llmAddrEntry, HintText: "OpenAI-compatible endpoint (Ollama, vLLM, ...)"},
			{Text: "LLM API Key", Widget: llmApiKeyEntry, HintText: "Leave empty for local models"},
			{Text: "Chat model", Widget: llmModelEntry, HintText: "Model name, e.g. gemma4:26b"},
		},
		OnSubmit: func() {
			result = NativeInputResult{
				OOSPUrl:      oospEntry.Text,
				DexURL:       dexEntry.Text,
				ClientID:     clientIDEntry.Text,
				VaultURL:     vaultEntry.Text,
				LLMAddr:      llmAddrEntry.Text,
				LLMApiKey:    llmApiKeyEntry.Text,
				LLMChatModel: llmModelEntry.Text,
				Aborted:      false,
			}
			close(done)
			a.Quit()
		},
		OnCancel: func() {
			close(done)
			a.Quit()
		},
		SubmitText: "Save",
		CancelText: "Cancel",
	}

	w.SetContent(container.NewVBox(errLabel, form, pingBtn))
	w.SetCloseIntercept(func() {
		select {
		case <-done:
		default:
			close(done)
		}
		a.Quit()
	})

	w.ShowAndRun()
	<-done
	return result
}
