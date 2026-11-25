# Roadmap

Where SetupMan is heading. Plans change with what testers report, so nothing below has a date.
Have an idea? [Suggest it](https://github.com/LaurensVR3/SetupMan/issues/new?template=4-idea.yml).

## In testing

- **Timed race end from the race leader** (ACC): fuel advice that knows you'll do an extra lap when the leader
  crosses the line just before the clock runs out.
- **Assetto Corsa on track**: lap recording, setup recognition and pit stop advice in AC need players to
  drive with them.
- **AC EVO and AC Rally setup values**: setting names and values compared with each game's setup screen.

## Planned

- **Telemetry demo pack**: real laps to explore lap analysis and the 3D track view before your first session.
- **Changing values in AC EVO**, once its setting names are confirmed by players.
- **Cloud backup** of the SetupMan library (OneDrive, Google Drive or Dropbox).
- **Live data for AC EVO** (lap recording, pit stop advice) **and AC Rally** (stage times per setup), once the
  games publish a documented interface.

## Being considered

- Telemetry for Assetto Corsa (AC writes no telemetry files, so SetupMan would record it itself).
- More games, for example Le Mans Ultimate, Automobilista 2 or iRacing.
- Real units for every setting of every car, and each car's limits when changing values.

## Done

- **0.24**: telemetry suite (ACC MoTeC files linked to sessions and setups, lap analysis of up to four laps,
  official turn numbers, time gained or lost per turn), 3D track view on real terrain, session and reference
  laps, race prep ranking setups by tyre temperature, pit stops planned with a standing time.
- **0.23**: public release, feedback forms, race prep (best setup for the session, fuel
  plan), setup recognition from the pit strategy, LFM next race.
- **Earlier**: setup library with every version and family trees, compare and similarity, change values,
  fuel calculator, lap recording, pit stop advice on a second screen, naming standard and auto-rename,
  temperature adjustment, sharing, folder management with backups, full backups, network access with PIN.
