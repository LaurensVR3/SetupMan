# SetupMan – notes for Claude

SetupMan manages simracing setups (ACC first). Requirements and all agreed decisions live in
`SetupMan Spec.md`. Read it before changing behaviour, and record every new decision in its D-xx table.
Label every question you ask the user (Q-xx), and move answers into the decisions table.
The owner is not a programmer: explain changes in plain language and keep code simple.

## Rules
- **Never write, move or delete anything in a user's setup folder** unless a spec decision allows it and the
  user confirmed the action (D-07). The only code that writes there is `internal/library/staging.go`
  (opt-in per game, D-73; also telemetry laps 99 on the user's button press, D-176), `internal/library/rename.go` (auto-rename merge of a new save into the renamed
  setup, only with folder management and auto-rename on, D-138) and `internal/folderbackup` restore. Keep it
  that way. The game's telemetry folder (ACC MoTeC) is never written (D-160).
- **Pure Go only** (no cgo – there is no C compiler on the dev machine). Standard library + `golang.org/x/sys`.
- **No frontend build step**: Vue 3 ES modules in `internal/server/web/`, vendored in `web/vendor/`,
  embedded with `go:embed`. No CDN links (app must work offline).
- **Performance (D-13)**: nothing may poll heavily. Idle CPU target < 0.5% of one core (measured ~0.05%).
- `SampleSetups/` (AC, ACC, AC EVO, AC Rally setups from the rig) is local and git-ignored. Never commit it.
  The dev PC's SetupMan points at `TestSetups/` (a git-ignored copy) so `SampleSetups/` is never written (D-184).
  Unit tests use the fixtures in `internal/games/acc/testdata/`; a few extra tests read `SampleSetups/` when present.
- **Don't edit files with PowerShell `Get-Content`/`WriteAllText`**: it corrupts non-ASCII characters.
  Use the Edit/Write tools.
- Nothing is pushed or published until the user asks (D-24). The public history starts at the fresh first
  commit on `main` (D-155); the old history is kept locally in branch `private-history` and must **never**
  be pushed or merged into `main`. Commits use the repository's git config (GitHub noreply address).
- **No personal details in tracked files (D-135)**: no IP addresses, machine names, home-network setup or the
  user's initials in the spec, manual, UI text, placeholders, tests or comments. Use neutral examples
  (initials `ABC`, addresses `192.168.1.x`). The user's own environment is in `Dev environment.local.md`
  (git-ignored); read it when network or rig details matter.
- Test history behaviour on an **isolated instance**: set `APPDATA` to a scratch folder (own settings +
  library), point it at a *copy* of `SampleSetups/`, run on another port (`--port 8766`). Never write to `SampleSetups/`.
- For UI checks use Chrome (Claude in Chrome); if the extension is "not connected", start Chrome first.

## Commands
- Test: `go test ./...`  (also `go vet ./...`)
- Build: `go build -ldflags "-H=windowsgui" -o SetupMan.exe ./cmd/setupman` (no console window; the UI is
  embedded, so rebuild after UI changes). Must also build with `CGO_ENABLED=0`.
- Run: `SetupMan.exe` (tray icon; `--no-browser`, `--no-tray` for development). Stop a running instance with
  `Stop-Process -Name SetupMan`. Log: `%APPDATA%\SetupMan\setupman.log`.
- On the dev machine, ACC is forced visible with the dump folder in Settings.
- Never toggle the real "Start with Windows" (HKCU Run key) while testing; tests use their own key.
- If `go` is not found in a fresh shell, refresh PATH (Go is in `C:\Program Files\Go\bin`).

## Layout
- `cmd/setupman` – entry point; the list of supported games is here
- `internal/game` – the `Game` interface every sim title implements, shared types
- `internal/games/gamekit` – shared by the newer games: folders (Documents, Saved Games, Local AppData, Steam), program check,
  `<car>/<track>/<file>` scanner, `ProcessLive`
- `internal/games/ac` (INI), `internal/games/ace` (protobuf, setting names in `labels.go`), `internal/games/acr`
  (one save file, read-only via `game.ReadOnly`)
- `internal/games/acc` – ACC: parse/flatten setups, scoring exclusions (`labels.go`), car/track names
  (`names.go`), folder layout scan, install detection, shared-memory driving detection
- `internal/setup` – game-neutral `Field` / `Setup` (with `Raw` file bytes for the library)
- `internal/naming` – reads initials / session / version / temperature / lap time / codes from names
- `internal/store` – SQLite library (`%APPDATA%\SetupMan\library.db`): setups, versions, changelog
- `internal/history` – version/branch/rename/removal matching, probable import, rebuild preview, relink
- `internal/compare` – similarity score, diffs, clustering
- `internal/library` – in-memory scans per game, folder watching, auto-rescan (paused while driving), page views;
  `staging.go` = manage on/off, stage/unstage/keep-only/keep, rename file, queue while driving, tidy on game close;
  `edit.go` (change values), `recorder.go` + `laps.go` (laps per setup), `rename.go` (suggested names,
  auto-rename), `tempadjust.go` (learn pressure/duct changes per °C from the user's own setups)
- `internal/fuel` – race / qualifying fuel plans
- `internal/folderbackup` – ACC setups folder backups (ZIP + manifest) and exact restore
- `internal/fullbackup` – full SetupMan backups (library snapshot + settings + folder backups); restore is applied
  at startup by `main.go` before the library opens (the server restarts SetupMan via `Server.Restart`)
- `internal/watch` – folder change notifications (Windows, no polling)
- `internal/lastopened` – reads setup files without changing their last-opened time, reads that time, checks the
  Windows setting (D-147). **Every read of a game's setups folder must use `lastopened.ReadFile` / `Open`**, or
  setup recognition breaks.
- `internal/motec` – reads MoTeC `.ld` files (header, channels, one lap at a time) and splits laps from ACC's TIME channel. ACC keeps most
  channels only for its last laps (they end at the last line crossing): always cut laps with `Channel.Window` after `File.Laps` (D-167)
- `internal/telemetry` – telemetry suite (D-159 … D-164): index MoTeC files, link to sessions, channel names, lap data,
  track map, turns, SetupMan's extra log (`.smx.gz`, D-162). ACC's MoTeC folder is **only read**; ACC re-saves a session
  repeatedly (each save a window) and names files after the save time: saves are put together per lap (D-163, D-168). Library glue in `internal/library/telemetry.go`; pages
  `TelemetryPage` (tracks), `TelemetryTrackPage` (one track, every car), `AnalysisPage` with canvas graphs in `components/TeleChart.js`, circuit outlines
  in `components/TrackOutline.js`. Look: timing-screen colours (purple fastest, green gain, red loss), no kerb stripes (D-165).
  `Telemetry/` (the user's MoTeC files, ~43 GB) is local and git-ignored; a few tests read it when present.
  Official turn numbers (D-171): `internal/telemetry/officialturns.go` is generated by `Telemetry/trackmaps/gen_go.py` from Wikimedia maps
  (`download*.py`, `convert.py` fits each map onto SetupMan's line via the test instance's API, writes `check_<track>.png` to check by eye).
- 3D track view (D-190): `internal/telemetry/geo.go` fits the game's track (extra-log positions, else the calculated line) onto the
  OpenStreetMap race track lines in `geotracks.go` (generated by `Telemetry/trackmaps/osm.py` + `gen_geo.py`); `internal/maps3d` downloads
  terrain (open Mapzen tiles) and offline government aerial photos (open licences only) to `%APPDATA%\SetupMan\maps`;
  `internal/library/geo3d.go` = scene per track (fit kept in KV) and each lap's path; page `components/Tele3D.js` (three.js in
  `web/vendor/`, loaded only for 3D). MapTiler photos (user's own key) are loaded by the browser and never stored (MapTiler terms).
- `internal/logos` – Wikimedia Commons logo download + cache
- `internal/settings` – `%APPDATA%\SetupMan\settings.json`
- `internal/server` – HTTP API, Server-Sent Events, embedded web UI (`web/`); `network.go` = LAN access with PIN
  (D-100, D-109): requests not from loopback need the PIN cookie and an IP-literal Host
- `internal/tray` – system tray icon + menu (fyne.io/systray, pure Go on Windows); icon drawn in code
- `internal/autostart` – "Start with Windows" via the per-user Run registry key

## Adding a game
Create `internal/games/<id>` implementing `game.Game` (and optionally `game.LiveReader`, `game.Shorter`,
`game.SetupFiles` for a file type other than .json, `game.ReadOnly`), then add it to the list in
`cmd/setupman/main.go`. Reuse `internal/games/gamekit`. Pages use the game's `short` name and `ext`, never "ACC". Class display order is in `internal/library/views.go`.
