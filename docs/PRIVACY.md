# Privacy and security

Short version: **SetupMan runs on your PC, keeps everything on your PC and has no account, no tracking and
no analytics.** It only goes online for the few optional things listed below, and only for that one thing.

## What SetupMan stores, and where

Everything is in `%APPDATA%\SetupMan` on your own PC:

| File or folder | What it holds |
|---|---|
| `settings.json` | Your settings (game folders, initials, optional LFM ID and MapTiler key, network PIN) |
| `library.db` | Every setup version, notes, family trees, laps and sessions |
| `backups\` | Backups of a game's setups folder (ZIP files) |
| `full-backups\` | Full SetupMan backups you made |
| `telemetry\` | SetupMan's optional extra telemetry log |
| `maps\` | Terrain and offline aerial photos for the 3D track view |
| `logos\` | Brand logos, cached |
| `setupman.log` | The log: what SetupMan saw of your games and your setup names |

Nothing is sent anywhere automatically. The log only leaves your PC if you attach it to a report yourself.

## What SetupMan reads and changes

- **Setups folders**: only read, until you enable **folder management** for a game. Enabling it makes a
  backup of the whole folder first. Reading never changes a file's "last opened" time.
- **ACC's MoTeC telemetry folder**: only read, never changed, moved or deleted.
- **ACC's broadcasting settings** (`broadcasting.json`): only read, to follow the race leader in timed
  races. SetupMan never sends commands to the game.
- **The game's shared memory** (ACC, AC): only read, while you drive, to record laps.
- **Windows registry**: only the **Start with Windows** entry, when you enable it.

## When SetupMan goes online

| What | Goes to | When | What is sent |
|---|---|---|---|
| Brand logos | Wikimedia Commons | The first time a car brand is shown; cached afterwards | The logo's file name |
| LFM next race | Low Fuel Motorsport | Only if you enter your LFM ID (Race prep) | Your LFM ID or a race number |
| 3D terrain | Mapzen terrain tiles (Amazon S3) | The first time you open a track in 3D; kept | The map tiles around the circuit |
| Offline aerial photos | The government map service of that country | Only when you choose to download them | The map tiles around the circuit |
| Satellite photos | MapTiler | Only if you enter your own MapTiler key and choose Satellite | Your key and the map tiles, loaded by your browser |
| Feedback buttons | GitHub | Only when you click one | Opens a form in your browser with the version filled in; nothing is sent until you submit it |

MapTiler photos stay in your browser's temporary cache only, as MapTiler's terms require; SetupMan never
stores them. SetupMan does not check for updates on its own.

## Network access (tablet, phone, second PC)

By default SetupMan only answers on your own PC (`127.0.0.1`). Nothing on your network can reach it.

When you enable **Settings → Network → Network access**, other devices on your network can open it:

- They must enter a **PIN** once (enabled by default). The PC SetupMan runs on never needs it.
- A device with the PIN has full use of SetupMan, Settings included. Treat the PIN like a Wi-Fi password.
- SetupMan only answers requests addressed by IP address or its own network name, which blocks web pages
  that try to reach it through a trick address (DNS rebinding). Changes are only accepted as JSON
  requests, which a web page on another site can't send to it.
- You can turn the PIN off (SetupMan asks first). Then every device on your network has full use.
- SetupMan uses plain HTTP, not HTTPS. Use network access on your home network only, never on public
  Wi-Fi, and allow SetupMan through the Windows firewall for **private** networks only.

## The program itself

- SetupMan is open source (MIT). Every release is built by GitHub from the public source and published with
  a SHA256 checksum (see [Installing](INSTALL.md#checking-the-download)).
- It is pure Go with three direct libraries (SQLite in Go, the tray icon and Windows system calls) plus
  Vue and three.js for the pages. All are included in the program: the pages load nothing from the internet.
