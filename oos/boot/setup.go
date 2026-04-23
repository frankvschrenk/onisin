package boot

// setup.go — First-run setup flow and settings dialog wiring.
//
// runSetupFlow is called when no oos.toml exists yet.
// OpenSettingsDialog is reachable from the main menu at any time.

import (
	"image/color"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos/helper"
)

// needsSetup returns true when no oos.toml exists yet.
func needsSetup() bool {
	return helper.NeedsSetup()
}

// runSetupFlow shows the first-run dialog and restarts the boot sequence on success.
func runSetupFlow() {
	current := helper.DefaultSetupConfig()

	result := helper.ShowSetupDialog(current)
	if result.Aborted {
		log.Println("[setup] cancelled — quitting")
		fyneApp.Quit()
		return
	}

	cfg := helper.SetupConfig{
		OOSPUrl:      result.OOSPUrl,
		DexURL:       result.DexURL,
		ClientID:     result.ClientID,
		VaultURL:     result.VaultURL,
		LLMAddr:      result.LLMAddr,
		LLMApiKey:    result.LLMApiKey,
		LLMChatModel: result.LLMChatModel,
	}
	if err := helper.SaveConfig(cfg); err != nil {
		log.Printf("[setup] save config: %v", err)
		helper.ShowErrorDialog("Error", "Could not save configuration: "+err.Error())
		fyneApp.Quit()
		return
	}

	if err := helper.LoadAppDirsConfig(); err != nil {
		log.Printf("[setup] load config: %v", err)
	}

	go runBoot()
}

// OpenSettingsDialog opens the settings dialog from the main menu.
// Changes take effect after the next restart.
func OpenSettingsDialog() {
	w := fyneApp.NewWindow("OOS Settings")
	w.Resize(fyne.NewSize(520, 580))
	w.CenterOnScreen()

	statusLabel := canvas.NewText("", color.RGBA{R: 50, G: 150, B: 50, A: 255})
	statusLabel.Alignment = fyne.TextAlignCenter
	statusLabel.TextStyle = fyne.TextStyle{Bold: true}

	oospEntry := widget.NewEntry()
	oospEntry.SetText(helper.Meta.OOSPUrl)
	oospEntry.SetPlaceHolder("http://localhost:9100")

	dexEntry := widget.NewEntry()
	dexEntry.SetText(helper.Meta.IAM.IssuerURL)
	dexEntry.SetPlaceHolder("http://localhost:5556")

	clientIDEntry := widget.NewEntry()
	clientIDEntry.SetText(helper.Meta.IAM.ClientID)
	clientIDEntry.SetPlaceHolder("oos-desktop")

	vaultEntry := widget.NewEntry()
	vaultEntry.SetText(helper.Meta.Vault.URL)
	vaultEntry.SetPlaceHolder("http://localhost:8200")

	llmAddrEntry := widget.NewEntry()
	llmAddrEntry.SetText(helper.LLMUrl)
	llmAddrEntry.SetPlaceHolder("http://localhost:11434")

	llmApiKeyEntry := widget.NewPasswordEntry()
	llmApiKeyEntry.SetText(helper.LLMApiKey)
	llmApiKeyEntry.SetPlaceHolder("leave empty for local models")

	llmModelEntry := widget.NewEntry()
	llmModelEntry.SetText(helper.LLMChatModel)
	llmModelEntry.SetPlaceHolder("e.g. gemma4:26b")

	pingBtn := widget.NewButton("Test OOSP", func() {
		ping := helper.PingOOSP(oospEntry.Text)
		if ping.OOSP != "ok" {
			statusLabel.Color = color.RGBA{R: 200, G: 50, B: 50, A: 255}
			statusLabel.Text = "OOSP: " + ping.OOSP
		} else {
			statusLabel.Color = color.RGBA{R: 50, G: 150, B: 50, A: 255}
			statusLabel.Text = "OOSP reachable"
		}
		statusLabel.Refresh()
	})

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
			cfg := helper.SetupConfig{
				OOSPUrl:      oospEntry.Text,
				DexURL:       dexEntry.Text,
				ClientID:     clientIDEntry.Text,
				VaultURL:     vaultEntry.Text,
				LLMAddr:      llmAddrEntry.Text,
				LLMApiKey:    llmApiKeyEntry.Text,
				LLMChatModel: llmModelEntry.Text,
			}
			if err := helper.SaveConfig(cfg); err != nil {
				log.Printf("[setup] save config: %v", err)
				statusLabel.Color = color.RGBA{R: 200, G: 50, B: 50, A: 255}
				statusLabel.Text = "Error: " + err.Error()
				statusLabel.Refresh()
				return
			}
			statusLabel.Color = color.RGBA{R: 50, G: 150, B: 50, A: 255}
			statusLabel.Text = "Saved"
			statusLabel.Refresh()
		},
		OnCancel:   func() { w.Close() },
		SubmitText: "Save",
		CancelText: "Cancel",
	}

	w.SetContent(container.NewVBox(statusLabel, form, pingBtn))
	w.Show()
}
