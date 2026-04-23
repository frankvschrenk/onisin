package boot

import (
	"fyne.io/fyne/v2"
	"onisin.com/oos/helper"
)

func openShellWindow() {
	openDashboard()

	helper.RenderFn = func(screenName string) {
		data := helper.Stage.CurrentData
		fyne.Do(func() {
			openOrRefreshBoardWindow(screenName, data)
		})
	}

	helper.ClearScreenFn = func() {
		fyne.Do(func() { closeAllBoardWindows() })
	}
}

func closeAllBoardWindows() {
	boardWindowsMu.Lock()
	wins := make([]*boardWindow, 0, len(boardWindows))
	for _, bw := range boardWindows {
		wins = append(wins, bw)
	}
	boardWindowsMu.Unlock()

	for _, bw := range wins {
		bw.win.Close()
	}
}
