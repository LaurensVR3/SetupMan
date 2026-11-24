// SetupMan – manage sim racing setups.
//
// Runs in the background with an icon in the Windows system tray (spec D-70) and serves
// its UI on 127.0.0.1, opened in the default browser. "Stop SetupMan" in the tray menu quits.
//
// Build without a console window:  go build -ldflags "-H=windowsgui" -o SetupMan.exe ./cmd/setupman
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"setupman/internal/autostart"
	"setupman/internal/folderbackup"
	"setupman/internal/fullbackup"
	"setupman/internal/game"
	"setupman/internal/games/ac"
	"setupman/internal/games/acc"
	"setupman/internal/games/ace"
	"setupman/internal/games/acr"
	"setupman/internal/library"
	"setupman/internal/logos"
	"setupman/internal/maps3d"
	"setupman/internal/server"
	"setupman/internal/settings"
	histstore "setupman/internal/store"
	"setupman/internal/tray"
	"setupman/internal/version"
)

func main() {
	port := flag.Int("port", 0, "port to listen on (default: Settings → Network, else 8765; a free port is used if it is taken)")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser (used when starting with Windows)")
	noTray := flag.Bool("no-tray", false, "run without the tray icon (stop with Ctrl+C); for development")
	flag.Parse()

	settingsPath, err := settings.DefaultPath()
	if err != nil {
		fail("SetupMan can't find a place for its settings: %v", err)
	}
	dataDir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fail("SetupMan can't create %s: %v", dataDir, err)
	}
	setupLog(filepath.Join(dataDir, "setupman.log"))
	log.Printf("SetupMan %s starting", version.Get())

	// Only this PC can connect, unless network access is switched on in Settings (D-100).
	bind := "127.0.0.1"
	nw := settings.NewStore(settingsPath).Load().Network
	if nw.Allow {
		bind = "0.0.0.0"
	}
	defaultPort := *port == 0
	if *port == 0 { // no --port option: the port chosen in Settings, else the usual one
		*port = 8765
		if nw.Port > 0 && nw.Port < 65536 {
			*port = nw.Port
		}
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bind, *port))
	if err != nil {
		// Port taken. If it's SetupMan already running, just open that one.
		existing := fmt.Sprintf("http://127.0.0.1:%d/", *port)
		if isSetupMan(existing) {
			log.Println("SetupMan is already running at", existing)
			if !*noBrowser {
				openBrowser(fmt.Sprintf("http://%s:%d/", server.LocalName, *port)) // the friendly address (D-116)
			}
			return
		}
		ln, err = net.Listen("tcp", bind+":0")
		if err != nil {
			fail("SetupMan can't start its web server: %v", err)
		}
	}

	// A full SetupMan backup waiting to be restored is put in place now: we are the only SetupMan
	// running (we hold the port) and the library isn't open yet (D-92). A backup of the current
	// state is made first.
	dbPath := filepath.Join(dataDir, "library.db")
	full := &fullbackup.Store{DataDir: dataDir}
	if applied, err := full.ApplyPending(func() error {
		cur, err := histstore.Open(dbPath)
		if err != nil {
			return err
		}
		defer cur.Close()
		_, err = full.Create(fullbackup.ReasonBeforeRestore, cur, nil)
		return err
	}, filepath.Join(dataDir, "backups")); err != nil {
		log.Printf("restoring a full backup failed: %v", err)
	} else if applied {
		log.Println("a full SetupMan backup was restored")
	}

	store := settings.NewStore(settingsPath)
	db, err := histstore.Open(dbPath) // every setup version (D-41)
	if err != nil {
		fail("SetupMan can't open its library: %v", err)
	}

	// Supported games. Adding a game = adding it here (see internal/game).
	games := []game.Game{acc.New(), ac.New(), ace.New(), acr.New()}
	lib := library.New(games, store, db)
	lib.Backups = &folderbackup.Store{Dir: filepath.Join(dataDir, "backups")} // ACC setups folder backups (D-82)
	lib.TelemetryDir = filepath.Join(dataDir, fullbackup.TelemetryDir)        // extra telemetry logs (D-162)
	lib.Maps = maps3d.New(filepath.Join(dataDir, "maps"))                     // 3D telemetry terrain and offline photos (D-190)
	lib.Start()
	if err := autostart.Refresh(); err != nil { // SetupMan.exe may have been moved
		log.Printf("start with Windows: %v", err)
	}

	srv := server.New(lib, store, logos.New(filepath.Join(dataDir, "logos")))
	httpSrv := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("web server: %v", err)
		}
	}()

	// Friendly address on this PC (D-116): http://setupman.localhost. Browsers send *.localhost to
	// this PC by themselves. SetupMan also listens on the standard web port 80 (this PC only) when
	// it is free, so no port number is needed; otherwise the address includes the port.
	url := fmt.Sprintf("http://%s:%d/", server.LocalName, ln.Addr().(*net.TCPAddr).Port)
	serve := func(l net.Listener) {
		go func() {
			if err := httpSrv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("web server (%s): %v", l.Addr(), err)
			}
		}()
	}
	// Browsers may try the IPv6 address of this PC first: listen there too.
	if bind == "127.0.0.1" {
		if l6, err := net.Listen("tcp", fmt.Sprintf("[::1]:%d", ln.Addr().(*net.TCPAddr).Port)); err == nil {
			serve(l6)
		}
	}
	// Port 80 only when it is free on both IPv4 and IPv6; another program (e.g. a local web
	// server) on either half would otherwise answer http://setupman.localhost.
	if defaultPort {
		if l4, err := net.Listen("tcp", "127.0.0.1:80"); err == nil {
			if l6, err := net.Listen("tcp", "[::1]:80"); err == nil {
				url = "http://" + server.LocalName + "/"
				serve(l4)
				serve(l6)
			} else {
				l4.Close()
			}
		}
	}
	log.Println("SetupMan is running at", url)
	if !*noBrowser {
		openBrowser(url)
	}

	var once sync.Once
	stop := func() {
		once.Do(func() {
			log.Println("SetupMan is stopping")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(ctx)
			lib.Stop()
			_ = db.Close()
		})
	}

	// Restart (after scheduling a full backup restore): stop cleanly so the port and the library
	// are free, start a new SetupMan with the same options, and quit. The open page reloads itself.
	srv.Full = full
	srv.Port, srv.Listening = ln.Addr().(*net.TCPAddr).Port, bind != "127.0.0.1"
	srv.Restart = func() {
		time.Sleep(300 * time.Millisecond) // let the answer reach the browser
		stop()
		exe, err := os.Executable()
		if err == nil {
			args := append([]string{}, os.Args[1:]...)
			if !*noBrowser {
				args = append(args, "--no-browser")
			}
			err = exec.Command(exe, args...).Start()
		}
		if err != nil {
			log.Printf("restart: %v", err)
		}
		os.Exit(0)
	}

	if *noTray {
		select {} // development: stop with Ctrl+C
	}
	tray.Run(tray.Options{Open: func() { openBrowser(url) }, Stop: stop, Lib: lib})
	stop()
}

// setupLog writes the log to a file (there is no console window). The file is started
// fresh when it grows beyond 1 MB.
func setupLog(path string) {
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(f, os.Stderr))
}

// fail shows an error to the user (a message box, since there is no console) and exits.
func fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Println(msg)
	showError(msg)
	os.Exit(1)
}

func isSetupMan(url string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url + "api/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		App string `json:"app"`
	}
	return resp.StatusCode == http.StatusOK &&
		json.NewDecoder(resp.Body).Decode(&body) == nil && body.App == "SetupMan"
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Println("could not open the browser, please open", url)
	}
}
