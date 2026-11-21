// Package ace supports Assetto Corsa EVO: setups are .carsetup files (protobuf) in
// Saved Games\ACE\Car Setups\<car name>\<track name>\<name>.carsetup.
package ace

import (
	"path/filepath"
	"strings"

	"setupman/internal/game"
	"setupman/internal/games/gamekit"
	"setupman/internal/setup"
)

// SteamAppID is AC EVO's Steam application id.
const SteamAppID = "3058630"

// Game is the AC EVO implementation of game.Game.
type Game struct {
	// The program names are not confirmed yet (Q-G2); any of them counts as "AC EVO runs".
	live gamekit.ProcessLive
}

// New creates the AC EVO game.
func New() *Game {
	// Program name confirmed by the user (Q-G2).
	return &Game{live: gamekit.ProcessLive{Exes: []string{"AssettoCorsaEVO.exe"}}}
}

func (g *Game) ID() string       { return "ace" }
func (g *Game) Name() string     { return "Assetto Corsa EVO" }
func (g *Game) Short() string    { return "AC EVO" }
func (g *Game) SetupExt() string { return ".carsetup" }

// Live reports whether AC EVO runs (driving isn't detected yet).
func (g *Game) Live() game.LiveState { return g.live.Live() }

// Detect looks for AC EVO via Steam and for its setups folder in Saved Games.
func (g *Game) Detect() game.Detection {
	var d game.Detection
	d.Installed, _ = gamekit.SteamGame(SteamAppID)
	if sg := gamekit.SavedGames(); sg != "" {
		if f := filepath.Join(sg, "ACE", "Car Setups"); gamekit.IsDir(f) {
			d.Folder, d.Installed = f, true
		}
	}
	return d
}

// ParseSetup turns stored file content back into comparable fields.
func (g *Game) ParseSetup(content []byte) ([]setup.Field, error) { return Parse(content) }

// Scan reads an AC EVO setups folder (read-only).
func (g *Game) Scan(folder string) (*game.Scan, error) {
	lay := gamekit.Layout{
		Ext:   ".carsetup",
		Parse: Parse,
		Car:   carInfo,
		Track: func(f string) string { return f }, // AC EVO already uses readable folder names
		Hint:  `No setups found. Choose the folder that contains the car folders (normally Saved Games\ACE\Car Setups).`,
	}
	return lay.Scan(folder)
}

// carInfo: AC EVO's folders are the readable car names, e.g. "Ferrari 296 GT3".
func carInfo(folder string) game.Car {
	c := game.Car{Name: folder, Brand: strings.Fields(folder + " ?")[0], Class: "Cars"}
	up := " " + strings.ToUpper(folder) + " "
	for _, cl := range []string{"GT2", "GT3", "GT4", "TCR", "LMP2", "LMP3", "LMH", "LMDH"} {
		if strings.Contains(up, " "+cl+" ") {
			c.Class = cl
			return c
		}
	}
	if strings.Contains(up, " CUP ") {
		c.Class = "Cup"
	}
	return c
}
