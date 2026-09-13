//go:build bindings

package main

import (
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// startup references this runtime-only helper. Binding generation never calls
// startup, so the bindings build uses a no-op implementation.
func (a *App) startAutoSync() {}

// The Wails CLI compiles and executes the application with the bindings build
// tag. Keep that process free of runtime side effects such as opening and
// migrating the user's database.
func main() {
	if err := wails.Run(&options.App{
		Bind: []interface{}{&App{}},
	}); err != nil {
		println("Failed to generate bindings:", err.Error())
	}
}
