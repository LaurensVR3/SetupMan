package tray

import (
	"fyne.io/systray"

	"setupman/internal/autostart"
	"setupman/internal/game"
	"setupman/internal/library"
)

// Options connect the tray icon to the rest of SetupMan.
type Options struct {
	Open func() // open the web UI in the browser
	Stop func() // shut SetupMan down cleanly (called once)
	Lib  *library.Library
}

// Run shows the tray icon and blocks until the user chooses "Stop SetupMan".
// It must be called from the main goroutine.
func Run(o Options) {
	systray.Run(func() { onReady(o) }, func() {})
}

func onReady(o Options) {
	systray.SetIcon(Icon())
	systray.SetTitle("SetupMan")
	systray.SetTooltip(tooltip(o.Lib.Live()))
	systray.SetOnTapped(o.Open) // left-click opens SetupMan

	open := systray.AddMenuItem("Open SetupMan", "Open SetupMan in your browser")
	systray.AddSeparator()
	start := systray.AddMenuItemCheckbox("Start with Windows", "Start SetupMan in the background when you sign in to Windows", autostart.Enabled())
	if !autostart.Supported {
		start.Disable()
	}
	systray.AddSeparator()
	stop := systray.AddMenuItem("Stop SetupMan", "Stop SetupMan (your setups are not affected)")

	events, cancel := o.Lib.Subscribe()
	go func() {
		defer cancel()
		for {
			select {
			case <-open.ClickedCh:
				o.Open()
			case <-start.ClickedCh:
				_ = autostart.Set(!autostart.Enabled())
				syncCheck(start)
				o.Lib.Notify(library.Event{Type: "autostart"})
			case e := <-events:
				switch e.Type {
				case "live":
					systray.SetTooltip(tooltip(e.Live))
				case "autostart": // changed on the Settings page
					syncCheck(start)
				}
			case <-stop.ClickedCh:
				systray.SetTooltip("SetupMan is stopping…")
				o.Stop()
				systray.Quit()
				return
			}
		}
	}()
}

func syncCheck(item *systray.MenuItem) {
	if autostart.Enabled() {
		item.Check()
	} else {
		item.Uncheck()
	}
}

func tooltip(s game.LiveState) string {
	switch s {
	case game.LiveDriving:
		return "SetupMan: driving, scanning paused"
	case game.LiveNotDriving:
		return "SetupMan: game running"
	default:
		return "SetupMan is running"
	}
}
