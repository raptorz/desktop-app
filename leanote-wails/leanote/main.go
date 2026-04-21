package main

import (
	"embed"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"leanote/db"
)

//go:embed all:frontend/dist
var assets embed.FS

func getLeanoteDataPath() string {
	homeDir, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "leanote")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		return filepath.Join(appData, "leanote")
	default:
		configDir := os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(homeDir, ".config")
		}
		return filepath.Join(configDir, "leanote")
	}
}

func main() {
	dataPath := getLeanoteDataPath()
	os.MkdirAll(dataPath, 0755)
	dbPath := filepath.Join(dataPath, "leanote.db")

	database, err := db.New(dbPath)
	if err != nil {
		println("Failed to initialize database:", err.Error())
		os.Exit(1)
	}

	app := NewApp(database)
	appMenu := buildMenu()

	protocolHandler := &leanoteProtocolHandler{app: app}

	err = wails.Run(&options.App{
		Title:     "Leanote",
		Width:     1050,
		Height:    595,
		MinWidth:  800,
		MinHeight: 500,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: protocolHandler,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Menu: appMenu,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.leanote.desktop",
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
			ProgramName: "Leanote",
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

type leanoteProtocolHandler struct {
	app *App
}

var fileIDRe = regexp.MustCompile(`fileId=([a-zA-Z0-9]{24})`)

func (h *leanoteProtocolHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch path {
	case "/file/getImage":
		fileIDs := fileIDRe.FindStringSubmatch(r.URL.RawQuery)
		if len(fileIDs) > 1 {
			imgResult := h.app.GetImage(fileIDs[1])
			if imgResult != nil && imgResult["Ok"] == true {
				if data, ok := imgResult["Data"].(string); ok {
					if ext, ok := imgResult["Type"].(string); ok {
						contentType := "image/png"
						switch ext {
						case "jpg", "jpeg":
							contentType = "image/jpeg"
						case "gif":
							contentType = "image/gif"
						case "svg":
							contentType = "image/svg+xml"
						case "webp":
							contentType = "image/webp"
						}
						w.Header().Set("Content-Type", contentType)
						w.Header().Set("Cache-Control", "public, max-age=31536000")
						decoded, err := decodeBase64(data)
						if err == nil {
							w.Write(decoded)
							return
						}
					}
				}
			}
		}
		http.NotFound(w, r)

	case "/file/getAttach":
		fileIDs := fileIDRe.FindStringSubmatch(r.URL.RawQuery)
		if len(fileIDs) > 1 {
			attachResult := h.app.GetAttach(fileIDs[1])
			if attachResult != nil && attachResult["Ok"] == true {
				if path, ok := attachResult["Path"].(string); ok {
					http.ServeFile(w, r, path)
					return
				}
			}
		}
		http.NotFound(w, r)

	default:
		http.NotFound(w, r)
	}
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func buildMenu() *menu.Menu {
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
	syncMenu.AddText("Sync Now", keys.CmdOrCtrl("s"), func(_ *menu.CallbackData) {})

	helpMenu := appMenu.AddSubmenu("Help")
	helpMenu.AddText("About", nil, func(_ *menu.CallbackData) {})

	return appMenu
}
