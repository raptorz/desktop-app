//go:build !systray
// +build !systray

package main

func RunSystray(app *App) {
	// Systray not enabled, no-op
}
