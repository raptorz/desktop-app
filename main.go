//go:build !bindings

package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/service"
	"github.com/gemsnote/gemsnote/webapi"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func getDataPath() string {
	homeDir, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "gemsnote")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		return filepath.Join(appData, "gemsnote")
	default:
		configDir := os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(homeDir, ".config")
		}
		return filepath.Join(configDir, "gemsnote")
	}
}

// migrateLegacyData silently adopts the pre-rename "leanote" data directory so upgrades keep their local data.
func migrateLegacyData(dataPath string) {
	legacy := filepath.Join(filepath.Dir(dataPath), "leanote")
	if _, err := os.Stat(filepath.Join(dataPath, "gemsnote.db")); err == nil {
		return
	}
	if _, err := os.Stat(filepath.Join(legacy, "leanote.db")); err != nil {
		return
	}
	_ = copyDir(legacy, dataPath)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

func main() {
	dataPath := getDataPath()
	os.MkdirAll(dataPath, 0755)
	migrateLegacyData(dataPath)
	dbPath := filepath.Join(dataPath, "gemsnote.db")

	database, err := db.New(dbPath)
	if err != nil {
		println("Failed to initialize database:", err.Error())
		os.Exit(1)
	}

	app := NewApp(database)
	app.sharedSync.OnRevocation = func(noteIDs []string) {
		if app.ctx != nil {
			wailsruntime.EventsEmit(app.ctx, "shared-notes-revoked", noteIDs)
		}
	}
	appMenu := buildMenu(app)

	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		println("Failed to access embedded frontend:", err.Error())
		os.Exit(1)
	}

	apiHandler := &webapi.Handler{
		DB:      database,
		Files:   service.NewFileService(database),
		Proxy:   webapi.NewServerProxy(database, app.files),
		Version: AppVersion,
		Dist:    dist,
		OnLogin: func() {
			// A server login must not reuse the previous account's cursors. The
			// personal full sync runs synchronously so the first workspace
			// render already sees the server snapshot; the shared cache refresh
			// stays independent in the background.
			_ = app.FullSyncForce()
			go app.sharedSync.SyncOnce()
		},
		OnLogout: func() error {
			user, _ := database.GetActiveUser()
			if user == nil || user.IsLocal || user.Host == "" || user.Token == "" {
				return nil
			}
			pending, err := database.HasPendingChanges(user.ID)
			if err != nil {
				return err
			}
			if !pending {
				return nil
			}
			_, err = app.sync.FullSync()
			return err
		},
		OnSync: func() (any, error) {
			return app.IncrSync(), nil
		},
		OnFullSync: func() (any, error) {
			return app.FullSyncForce(), nil
		},
		OnSharedDownload: func() { go app.sharedSync.DownloadPending() },
	}

	err = wails.Run(&options.App{
		Title:     "Gemsnote 珠玑笔记",
		Width:     1050,
		Height:    595,
		MinWidth:  800,
		MinHeight: 500,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: apiHandler,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Menu: appMenu,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.gemsnote.desktop",
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				app.ShowWindow()
				if len(secondInstanceData.Args) > 1 {
					deepLink := secondInstanceData.Args[1]
					if deepLink != "" {
						app.HandleDeepLink(deepLink)
					}
				}
			},
		},
		Linux: &linux.Options{
			ProgramName: "gemsnote",
			Icon:        appIcon,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

func buildMenu(app *App) *menu.Menu {
	appMenu := menu.NewMenu()

	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("New Note", keys.CmdOrCtrl("n"), func(_ *menu.CallbackData) {})
	fileMenu.AddText("New Notebook", keys.CmdOrCtrl("shift+n"), func(_ *menu.CallbackData) {})
	fileMenu.AddSeparator()
	fileMenu.AddText("Export PDF", keys.CmdOrCtrl("shift+e"), func(_ *menu.CallbackData) {})
	fileMenu.AddSeparator()
	fileMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		os.Exit(0)
	})

	editMenu := appMenu.AddSubmenu("Edit")
	editMenu.AddText("Undo", keys.CmdOrCtrl("z"), func(_ *menu.CallbackData) {})
	editMenu.AddText("Redo", keys.CmdOrCtrl("shift+z"), func(_ *menu.CallbackData) {})
	editMenu.AddSeparator()
	editMenu.AddText("Cut", keys.CmdOrCtrl("x"), func(_ *menu.CallbackData) {})
	editMenu.AddText("Copy", keys.CmdOrCtrl("c"), func(_ *menu.CallbackData) {})
	editMenu.AddText("Paste", keys.CmdOrCtrl("v"), func(_ *menu.CallbackData) {})
	editMenu.AddText("Select All", keys.CmdOrCtrl("a"), func(_ *menu.CallbackData) {})

	viewMenu := appMenu.AddSubmenu("View")
	viewMenu.AddText("Toggle Full Screen", keys.Key("F11"), func(_ *menu.CallbackData) {})

	syncMenu := appMenu.AddSubmenu("Sync")
	syncMenu.AddText("Sync Now", keys.CmdOrCtrl("s"), func(_ *menu.CallbackData) {
		go app.IncrSync()
	})
	syncMenu.AddText("Full Sync", keys.CmdOrCtrl("shift+s"), func(_ *menu.CallbackData) {
		go app.FullSyncForce()
	})

	helpMenu := appMenu.AddSubmenu("Help")
	helpMenu.AddText("About", nil, func(_ *menu.CallbackData) {})

	return appMenu
}

func (a *App) startAutoSync() {
	go func() {
		if user, _ := a.db.GetActiveUser(); user != nil && user.Token != "" {
			a.IncrSync()
			a.sharedSync.SyncOnce()
		}
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		sharedTicker := time.NewTicker(5 * time.Minute)
		defer sharedTicker.Stop()
		for {
			select {
			case <-ticker.C:
				if user, _ := a.db.GetActiveUser(); user != nil && user.Token != "" && !a.IsSyncing() {
					a.IncrSync()
				}
			case <-sharedTicker.C:
				if user, _ := a.db.GetActiveUser(); user != nil && user.Token != "" {
					a.sharedSync.SyncOnce()
				}
			}
		}
	}()
}
