# SetupMan

Manage simracing setups. Free and open source (MIT).

**Status: early version, Windows only.** Assetto Corsa Competizione has been tested on a real sim rig; Assetto Corsa, Assetto Corsa EVO and Assetto Corsa Rally are built from public documentation and still need testing by players. Your feedback helps: see [Feedback](#feedback).

Supported: Assetto Corsa Competizione (everything), Assetto Corsa (setups, changing values, lap recording, pit stop advice), Assetto Corsa EVO (read, compare, history, choosing what the game shows), Assetto Corsa Rally (read, compare, history; read-only because all its setups share one save file). Settings → About in the app has a table of the features per game.
- Shows your setups per game, car and track, with readable names and brand logos.
- Finds identical and near-identical setups and compares them side by side.
- Keeps every version of every setup, shows where a setup came from (family tree with Q / R / endurance
  branches) and lets you add notes. Your existing setups get a *probable* history, which you can correct.
- Updates automatically when setups change, but not while you are driving.
- **Change values** of a setup in clicks, like in the game, and save it as a new version or a new setup.
- **Fuel calculator** for races (stops, pit window) and qualifying, per car and track.
- **Session reports**: every practice, qualifying or race session on one page, with stints and every lap
  (time, fuel, temperatures, hot tyre pressures). Open one from a setup's Lap times tab.
- **Pit stop advice** during an ACC session (top bar → Pit stop): fuel to add and tyre pressures for the next
  stop, from your last laps (targets 27.0 psi dry, 30.0 psi wet). Made for a second screen or tablet.
- **Records your laps** while ACC or AC runs: best and average lap, fuel per lap and track temperature per setup
  version. The setup you drive is recognised from what the car reports (in ACC: fuel, the pit
  strategy's tyre pressures, TC, ABS, engine map; in AC also the setup file the game opened last). When setups
  fit equally well, **Manual setup select** sets it with one click.
- **Naming standard**: suggested names per setup or for a whole game at once (with a preview), rename by
  hand, or apply it to new in-game saves automatically (opt-in). The track page shows each setup's game version.
- **Adjust for temperature**: proposes tyre pressure and brake duct changes, learned from your own setups.
- **Share** a setup as one file that a friend opens in their SetupMan (setup page → Share…).
- **Export and import** setups as ZIP files (track page), and **full SetupMan backups** of everything
  (Settings → Backups), which you can download and restore, also on another PC.
- Optionally manages the game's setups folder so ACC only shows the setups you pick (see below).

## Folder management

By default SetupMan only *reads* your setups folder. When you enable **Settings → Games → Folder management**:

1. SetupMan first creates a backup of the whole folder and verifies that every setup is stored in its library.
2. On a track page you select which setups the game lists (**Show only these in ACC**); the others are hidden
   from the game but stay stored in SetupMan, and can be shown again at any time (also older versions).
3. New setups you save in ACC are listed as "New setups saved in ACC", so you can keep or hide them.
4. Nothing changes while you are driving; changes wait until you stop.

**Settings → Backups** lists your ACC setups folder backups. **Restore** puts the folder back exactly as it
was at that moment (every file, name and date); SetupMan creates a backup of the current state first, so a
restore can be undone too. These backups contain only the game folder, not SetupMan's own history and notes.

See `SetupMan Spec.md` for the full plan and all agreed decisions.

## Run

1. Double-click `SetupMan.exe`. The app opens in your browser at http://setupman.localhost (with the port
   number, e.g. :8765, when port 80 is in use), and SetupMan's icon appears among the hidden icons next to the
   clock (click the `^` arrow if you don't see it).
2. Installed games are found automatically. If yours isn't, open **Settings**, set the game to
   "Always" and choose its setups folder.
3. The tray icon: left-click opens SetupMan; right-click has **Start with Windows** and **Stop SetupMan**.
   Closing the browser does not stop SetupMan.
4. To use SetupMan from another PC, laptop or tablet: **Settings → Network**, enable **Network access**,
   open the address shown there on the other device and type the PIN once. To use a name such as
   http://setupman.lan with Nginx Proxy Manager, see the Manual in the app (Other devices and setupman.lan).

Options (from a terminal):

```
SetupMan.exe --port 9000      use another port for this run (normally set in Settings → Network; default 8765)
SetupMan.exe --no-browser     don't open the browser (used by Start with Windows)
SetupMan.exe --no-tray        no tray icon; stop with Ctrl+C (for development)
```

If something goes wrong, see `%APPDATA%\SetupMan\setupman.log`.

SetupMan's own files live in `%APPDATA%\SetupMan`:
- `settings.json`: your settings;
- `library.db`: every setup version, notes and history (back this up if you care about it);
- `backups\`: ACC setups folder backups (ZIP files);
- `full-backups\`: full SetupMan backups;
- `logos\`: cached brand and game logos.

The manual (top bar → Manual) explains every feature.

## How "similar" is calculated

Every setting counts as one field (each wheel separately). The score is the percentage of fields that are
identical. Shown but not scored:
- the car name and the track BoP type;
- telemetry laps, the selected tyre set, and ACC's own fuel-per-lap estimate;
- values the game calculates itself: static camber, linear toe and rod length.

Unused pit-strategy slots are ignored.

## Build from source

Requires Go 1.27+ (`winget install GoLang.Go`). No other tools: the web UI uses Vue 3 from
`internal/server/web/vendor` without a build step.

```
go test ./...
go build -ldflags "-H=windowsgui" -o SetupMan.exe ./cmd/setupman
```

(`-H=windowsgui` builds without a console window; leave it out to see log output in a console.)

## Feedback

SetupMan is tested on one sim rig so far, mostly with ACC. Reports from players make it better quickly,
especially for Assetto Corsa, AC EVO and AC Rally. The easiest way: in SetupMan, **Settings → About →
Feedback**. Each button opens a short form with your version filled in:

- **Report a problem**: something is wrong, missing or doesn't start;
- **A value differs from the game**: a number on the Setup tab doesn't match the game's setup screen;
- **Send a test report**: tick what worked with a game (about ten minutes of driving);
- **Suggest an idea**.

Or [open an issue](https://github.com/LaurensVR3/SetupMan/issues/new/choose) here directly. The log file helps
most: Settings → About → **Open log folder**, then drag `setupman.log` into the form. It lists what SetupMan
saw of your games and your setup names, nothing else personal.

## Credits

- [Vue](https://vuejs.org) and [Vue Router](https://router.vuejs.org) (MIT). Licences are in `internal/server/web/vendor`.
- Real setup values for every ACC car: the numbers from the open-source [Race Element](https://github.com/RiddleTime/Race-Element) project (GPL-3; only its data is used, not its code).
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (pure Go SQLite), [fyne.io/systray](https://github.com/fyne-io/systray) and [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys).
- Brand logos are downloaded from [Wikimedia Commons](https://commons.wikimedia.org) when SetupMan runs. They are trademarks of
  their owners and are only used to identify the cars.
