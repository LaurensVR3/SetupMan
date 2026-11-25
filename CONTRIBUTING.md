# Contributing to SetupMan

Thanks for helping. The most useful contribution right now is **testing with your own games**:
see [Feedback in the README](README.md#feedback). Ideas and questions are welcome too.

## Reporting

- **Something wrong, or a value that differs from the game**: use the forms under
  [New issue](https://github.com/LaurensVR3/SetupMan/issues/new/choose), or Settings → About → Feedback in the
  app (fills in your version). Attach `setupman.log` (Settings → About → Open log folder).
- **Questions, ideas, your setup workflow**: [Discussions](https://github.com/LaurensVR3/SetupMan/discussions).

## Building

You need Windows and [Go](https://go.dev/dl/) (the version in `go.mod`; `winget install GoLang.Go`).
Nothing else: no C compiler, no Node.js.

```
go vet ./...
go test ./...
go build -ldflags "-H=windowsgui" -o SetupMan.exe ./cmd/setupman
```

Leave out `-ldflags "-H=windowsgui"` to see the log in a console. For development, run
`SetupMan.exe --no-browser --no-tray --port 8766`.

To try changes without touching your real data, give the test run its own data folder and a **copy** of your
setups:

```powershell
$env:APPDATA = "C:\Temp\setupman-test"
.\SetupMan.exe --no-tray --port 8766
```

Then point Settings → Games at a copy of your setups folder.

## Ground rules

These keep SetupMan safe and simple. A pull request that breaks one can't be merged.

- **Never write to a player's setups folder** unless folder management is enabled for that game. The only
  code that writes there is in `internal/library/staging.go`, `internal/library/rename.go` and the backup
  restore in `internal/folderbackup`. Every read of a setups folder goes through `internal/lastopened`.
- **Pure Go**: standard library and `golang.org/x/sys` (plus the libraries already in `go.mod`). No cgo: it
  must build with `CGO_ENABLED=0`.
- **No frontend build step**: the pages are Vue 3 ES modules in `internal/server/web/`, with libraries
  vendored in `web/vendor/` and built into the program. No CDN links: SetupMan works offline.
- **Light on the PC**: nothing polls heavily; idle CPU stays well under 0.5 % of one core. Players run
  this next to a sim.
- **Plain language** in the app, the Manual and the docs: written for racers, not programmers.
- **No personal details** in files, tests or commits (IP addresses, machine names, real names).

## Finding your way

| Folder | What's there |
|---|---|
| `cmd/setupman` | Start-up; the list of supported games |
| `internal/game` | The `Game` interface every sim implements |
| `internal/games/acc`, `ac`, `ace`, `acr` | One package per game: reading setups, names, live data |
| `internal/games/gamekit` | Shared helpers for games (folders, scanning) |
| `internal/library` | Scanning, folder watching, folder management, laps, renaming |
| `internal/store` | The SQLite library: setups, versions, changelog |
| `internal/history`, `internal/compare` | Version matching, similarity and differences |
| `internal/fuel` | Fuel plans for race and qualifying |
| `internal/motec`, `internal/telemetry` | Reading MoTeC files, lap analysis, track maps, 3D placement |
| `internal/server` | The web server and API; the pages in `internal/server/web/` |
| `internal/folderbackup`, `internal/fullbackup` | Backups and restore |

[`SetupMan Spec.md`](SetupMan%20Spec.md) holds every design decision (the D-xx table). If a change alters
how SetupMan behaves, add a decision there. [`CLAUDE.md`](CLAUDE.md) has the same rules in short form.

### Adding a game

Create `internal/games/<id>` implementing `game.Game` (optional extras: `game.LiveReader` for live data,
`game.SetupFiles` for a file type other than `.json`, `game.ReadOnly`), reuse `internal/games/gamekit`, and
add it to the list in `cmd/setupman/main.go`. Pages use the game's short name, never "ACC". Please include a
few real setup files as test fixtures in `testdata/` (your own setups only, not someone else's).

## Pull requests

1. Open an issue first for anything bigger than a small fix, so we can agree on the approach.
2. Keep a pull request to one change, with tests for parsing, comparing or calculating code.
3. `go vet ./...` and `go test ./...` must pass; GitHub runs them on every pull request.
4. By contributing you agree your work is published under the [MIT licence](LICENSE).
