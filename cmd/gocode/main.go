// Command gocode is the native-UI desktop build: a single Go binary that
// boots the upstream backend (src-go web server) in-process on a loopback port
// and renders the workspace in a mygo native-UI window — the Go analog of the
// upstream Tauri + Next.js shell (src-tauri + src/app).
//
// Run a live-reloading dev preview with:
//
//	go tool mygo dev ./cmd/gocode     # rebuilds + relaunches on change
//
// It opens a real desktop window (WebView2/GPU), so it needs a display; a
// headless CI box can build it but not show it.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gocode/src/app"
	"gocode/src/documents"

	appstate "gocode/src-go/app_state"
	"gocode/src-go/db"
	"gocode/src-go/web"
	"gocode/src-go/web/handlers"

	// The catalogs' languages become the app's: importing the generated
	// package registers them with package i18n (and links MyGo's own
	// translations for them).
	"gocode/locales"
)

// bootBackend starts the web server on 127.0.0.1:<random> and returns an
// app.Backend bound to it. It serves in a background goroutine for the app's
// lifetime, using the exact HTTP contract mcode-app speaks.
func bootBackend() (*app.Backend, error) {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".gocode")
	if env := os.Getenv("GOCODE_DATA_DIR"); env != "" {
		dataDir = env
	}
	// Apply a staged backup restore BEFORE opening the DB: the swap replaces the
	// DB file + managed sections, so it must land ahead of the connection.
	if applied, aerr := handlers.ApplyPendingRestoreAtStartup(dataDir); aerr != nil {
		log.Printf("[gocode] apply pending restore: %v", aerr)
	} else if applied.Applied {
		log.Printf("[gocode] staged restore applied; prior data preserved at %s", applied.SafetySnapshot)
	}
	database, err := db.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("db init: %w", err)
	}
	st := appstate.Init(database, dataDir)

	token := strings.ReplaceAll(fmt.Sprintf("gocode-%d", time.Now().UnixNano()), "-", "")
	router := web.NewRouter(st)
	router.SetToken(token)
	handlers.RegisterAll(handlers.NewRegistry(router))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	st.Server.MarkRunning(addr.IP.String(), uint16(addr.Port), token)
	// Let the ACP engine offer the gocode-mcp companion to agents it spawns,
	// with a working loopback API + bearer token baked into its environment.
	st.ACP.SetCompanionBackend(fmt.Sprintf("http://%s:%d", addr.IP, addr.Port), token)
	go func() {
		srv := &http.Server{Handler: router}
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[gocode] backend serve: %v", err)
		}
	}()
	return &app.Backend{URL: fmt.Sprintf("http://%s:%d", addr.IP, addr.Port), Token: token}, nil
}

func main() {
	b, err := bootBackend()
	if err != nil {
		log.Fatalf("[gocode] %v", err)
	}
	shell := app.New(b)

	// Install the NSDocument-style model: File menu (New / Open / Open Recent /
	// Save / Save As / Duplicate / Rename / Move To / Revert), a window per
	// Markdown/Text file, autosave in place, crash recovery, save prompts on
	// close/quit, and detection of files changed by other apps. The main window
	// stays the app's primary surface.
	documents.Register()

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   locales.AppTitle(), // said again when the language changes
			Width:   1240,
			Height:  820,
			Content: ui.View(shell.View),
		})
		// The window's title is set once; hear the language change and say it
		// again, the way the i18n docs describe for one-time labels.
		mygo.App.OnLocaleChanged(func(string) {
			win.SetTitle(locales.AppTitle())
		})
		// The floating pet window brings the workspace forward when its badge is
		// clicked, and comes back at launch if it was open last time.
		shell.SetMainWindow(win)
		shell.RestorePetWindow()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
