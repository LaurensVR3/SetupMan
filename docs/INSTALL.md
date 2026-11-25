# Installing SetupMan

SetupMan is one program file, `SetupMan.exe`. There is no installer: it runs from any folder and keeps its
own data in `%APPDATA%\SetupMan`. It needs Windows 10 or 11 (64-bit) and a web browser.

- [Fresh installation](#fresh-installation)
- [The Windows warnings](#the-windows-warnings)
- [Upgrading](#upgrading)
- [Checking the download](#checking-the-download)
- [Using a tablet or phone](#using-a-tablet-or-phone)
- [Uninstalling](#uninstalling)
- [Problems](#problems)

## Fresh installation

1. Download `SetupMan.exe` from the [latest release](https://github.com/LaurensVR3/SetupMan/releases/latest).
2. Make a folder for it, for example `C:\Tools\SetupMan`, and move `SetupMan.exe` there. Any folder you
   can write to works; SetupMan itself writes nothing next to the program.
3. Double-click `SetupMan.exe`. Windows may warn you first: see [the Windows warnings](#the-windows-warnings).
4. Your browser opens SetupMan at http://setupman.localhost. Installed games are found automatically.
   If yours isn't, open **Settings → Games**, set the game to **Always** and choose its setups folder.
5. SetupMan's icon appears next to the clock (click the `^` arrow if it is hidden). Left-click opens
   SetupMan; right-click has **Start with Windows** and **Stop SetupMan**. Closing the browser does not
   stop SetupMan.

SetupMan only **reads** your setups folder until you enable folder management for a game
(Settings → Games). Enabling it makes a backup of the whole folder first.

## The Windows warnings

SetupMan is not signed with a paid code-signing certificate, so Windows and your browser don't know it
yet. You may see:

- **In the browser** (Edge or Chrome): "SetupMan.exe isn't commonly downloaded". Open the downloads list,
  click **…** next to the file and choose **Keep** (Edge: **Show more → Keep anyway**).
- **Windows SmartScreen** when you start it: "Windows protected your PC". Click **More info**, then
  **Run anyway**. Windows remembers this for that file.

If you want to be sure the file is the one built by GitHub from the public source, check it first
(see [Checking the download](#checking-the-download)).

## Upgrading

Your setups, history, notes, laps and settings are in `%APPDATA%\SetupMan`, not in the program file, so an
upgrade keeps everything.

1. Download the new `SetupMan.exe`.
2. Stop SetupMan: right-click the tray icon → **Stop SetupMan**.
3. Replace the old `SetupMan.exe` with the new one (same folder, so **Start with Windows** keeps working).
4. Start it. **Settings → About** shows the new version number.

Want a safety net? Before upgrading, make a full backup in **Settings → Backups & bin**.
**Settings → About → Latest version** opens the releases page; SetupMan does not check for updates by itself.

## Checking the download

Every release lists `SHA256SUMS.txt` next to `SetupMan.exe`, made by GitHub when it built the program.
To compare, open PowerShell in your download folder and run:

```powershell
Get-FileHash .\SetupMan.exe -Algorithm SHA256
```

The long number must be the same as the one in `SHA256SUMS.txt` (upper or lower case doesn't matter).
GitHub also shows the number next to each file on the release page.

## Using a tablet or phone

The pit stop advice and the fuel plan are made for a second screen. In **Settings → Network**, enable
**Network access**, open the address shown there on the other device and type the PIN once. Windows may ask
to allow SetupMan through the firewall: allow it for **private** networks only. See
[Privacy and security](PRIVACY.md) for what this opens up.

## Uninstalling

1. **If folder management is enabled** for a game, decide what the game folder should look like without
   SetupMan. Setups SetupMan hid from the game are stored only in SetupMan's library. Either:
   - show the setups you want in the game first (track page → **Show in …**), or
   - put the folder back exactly as it was before SetupMan managed it: **Settings → Backups & bin**, the
     backup made "before managing", **Restore**.
2. Right-click the tray icon, turn **Start with Windows** off, then **Stop SetupMan**.
3. Delete `SetupMan.exe`.
4. To remove all of SetupMan's data as well, delete the folder `%APPDATA%\SetupMan` (paste that into the
   Explorer address bar). This removes your setup history, notes, laps and backups: keep a full backup
   somewhere else if you might come back.

SetupMan never changes the Windows registry except for the **Start with Windows** entry, and never changes
the game's MoTeC telemetry files.

## Problems

- **Nothing opens**: look for the tray icon. If it is there, left-click it. The log is in
  `%APPDATA%\SetupMan\setupman.log`.
- **Port in use**: SetupMan picks another port by itself and shows it in the address bar. To choose one,
  start it with `SetupMan.exe --port 9000`.
- Something else? **Settings → About → Feedback**, or
  [open an issue](https://github.com/LaurensVR3/SetupMan/issues/new/choose) and attach `setupman.log`.
