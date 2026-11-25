# Game compatibility

What SetupMan can do per game, and how well each part has been tested. The same feature table is in the
app (Settings → About).

## Testing status

| Game | Status | Tested with |
|---|---|---|
| **Assetto Corsa Competizione** | **Tested on a sim rig**: four test rounds and online races | ACC 1.10.4 (Steam), November 2026 |
| **Assetto Corsa** | Built and unit-tested on real setup files; **needs players** to drive with it | Setup files from a real installation |
| **Assetto Corsa EVO** | Built from real setup files and public information; **needs players** | Setup files from a real installation |
| **Assetto Corsa Rally** | Built from a real save file; **needs players** | One save file from a real installation |

Tested with another version, or found something that differs? Please send a
[test report](https://github.com/LaurensVR3/SetupMan/issues/new?template=3-test-report.yml): it takes about
ten minutes of driving and is the fastest way to move a game up this table.

## Features per game

| Feature | ACC | AC | AC EVO | AC Rally |
|---|:-:|:-:|:-:|:-:|
| Read, compare and keep every version | ✓ | ✓ | ✓ | ✓ |
| Change values | ✓ | ✓ | – | – |
| Values in real units (degrees, mm, N/m) | ✓ | With AC installed | ✓ | As stored |
| Choose which setups the game lists (folder management) | ✓ | ✓ | ✓ | – |
| Rename, remove duplicates, naming standard | ✓ | ✓ | ✓ | – |
| Record laps and recognise the setup driven | ✓ | No valid-lap flag | – | – |
| Pit stop advice (second screen) | ✓ | ✓ | – | – |
| Adjust for temperature | ✓ | ✓ | – | – |
| Race prep and fuel calculator | ✓ | ✓ | ✓ | – |
| Share and import | ✓ | ✓ | ✓ | – |
| Download and backups | ✓ | ✓ | ✓ | ✓ |
| Telemetry: lap analysis from MoTeC files | ✓ | – | – | – |
| 3D track view | ✓ | – | – | – |
| Timed race end from the race leader | ✓ | – | – | – |

Notes:

- **AC Rally** keeps all setups in one save file, so SetupMan only reads them (it never writes that file).
  It is a rally game: no pit stops, so no fuel plans or pit stop advice.
- **AC real units** come from the car data of the installed game; on a PC without AC the values stay as
  stored in the setup file.
- **AC lap recording**: AC does not report whether a lap was valid, so every lap counts.
- **AC EVO and AC Rally live data** (lap recording; pit stop advice for AC EVO) waits until the games publish a
  documented interface. SetupMan does not guess.
- **Telemetry** reads ACC's own MoTeC files (only read, never changed). AC writes no telemetry files.
- **3D track view**: 24 circuits that have race track lines in OpenStreetMap; Mount Panorama does not.

See the [roadmap](ROADMAP.md) for what is planned per game.
