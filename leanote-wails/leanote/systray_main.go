//go:build systray
// +build systray

package main

import (
	_ "embed"

	"fyne.io/systray"
)

//go:embed frontend/dist/public/images/logo_64.png
var iconData []byte

func RunSystray(app *App) {
	systray.Run(func() {
		systray.SetIcon(iconData)
		systray.SetTitle("Leanote")
		systray.SetTooltip("Leanote - Not Just A Notepad")

		mShow := systray.AddMenuItem("Show", "Show main window")
		mSync := systray.AddMenuItem("Sync Now", "Start sync")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit application")

		go func() {
			for {
				select {
				case <-mShow.ClickedCh:
					app.ShowWindow()
				case <-mSync.ClickedCh:
					app.IncrSync()
				case <-mQuit.ClickedCh:
					app.StopSync()
					app.shutdown(nil)
					systray.Quit()
				}
			}
		}()
	}, func() {
		// Cleanup
	})
}
