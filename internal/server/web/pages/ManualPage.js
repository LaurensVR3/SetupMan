// The SetupMan manual (owner request 2026-11-23: "restructure into cleanly organised tabs"): one tab
// per area, each with its own contents list. ?tab=<id> opens a tab, &s=<section> jumps to a section.
import { computed, watch, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import { setCrumbs } from "../components/common.js";

const TABS = [
  { id: "start", label: "Getting started", sections: [["welcome", "What SetupMan does"], ["games", "Supported games"], ["find", "Finding a setup"]] },
  { id: "setups", label: "Setups", sections: [["values", "Seeing and changing values"], ["history", "Versions and families"], ["compare", "Comparing setups"],
    ["duplicates", "Duplicates"], ["folder", "Choosing what the game shows"], ["names", "Names"], ["temp", "Adjusting for temperature"], ["share", "Sharing, importing and downloading"]] },
  { id: "racing", label: "Driving and racing", sections: [["laps", "Lap times and sessions"], ["recognition", "Which setup you drive"], ["prep", "Race prep"],
    ["lfm", "Low Fuel Motorsport"], ["fuel", "Fuel calculator"], ["pit", "Pit stop advice"], ["weekend", "A race weekend, step by step"]] },
  { id: "telemetry", label: "Telemetry", sections: [["tele-files", "Where it comes from"], ["tele-find", "Finding a lap"], ["tele-analysis", "Lap analysis"],
    ["tele-turns", "Turn numbers"], ["tele-brakes", "Brakes over a session"], ["tele-extra", "Extra telemetry log"]] },
  { id: "backups", label: "Backups", sections: [["backup-kinds", "Two kinds of backup"], ["backup-where", "Where backups are"], ["log", "Log file"]] },
  { id: "devices", label: "Other devices", sections: [["network", "Phone, tablet and other PCs"], ["lan", "setupman.lan instead of an IP address"], ["lan-trouble", "If it does not work"]] },
  { id: "words", label: "Words", sections: [["words", "Words used in SetupMan"]] },
];

export const ManualPage = {
  setup() {
    setCrumbs([{ label: "Manual" }]);
    const route = useRoute(), router = useRouter();
    const tab = computed(() => (TABS.some((t) => t.id === route.query.tab) ? route.query.tab : "start"));
    const current = computed(() => TABS.find((t) => t.id === tab.value));
    const setTab = (id) => router.replace({ query: { tab: id === "start" ? undefined : id } });
    const scroll = (s) => nextTick(() => setTimeout(() => document.getElementById("m-" + s)?.scrollIntoView({ behavior: "smooth", block: "start" }), 60));
    // a section, also on another tab
    function go(s) {
      const t = TABS.find((x) => x.sections.some((y) => y[0] === s));
      if (t && t.id !== tab.value) router.replace({ query: { tab: t.id === "start" ? undefined : t.id, s } });
      else scroll(s);
    }
    watch(() => route.query.s, (s) => s && scroll(s), { immediate: true });
    return { TABS, tab, current, setTab, go };
  },
  template: `
    <main class="page manual">
      <h1 class="page-title">Manual</h1>
      <p class="page-sub">How SetupMan works, from finding a setup to analysing your laps.</p>
      <nav class="tabs manual-tabs" role="tablist" aria-label="Manual">
        <button v-for="t in TABS" :key="t.id" type="button" role="tab" :aria-selected="tab === t.id" :class="{ on: tab === t.id }" @click="setTab(t.id)">{{ t.label }}</button>
      </nav>
      <div class="manual-layout">
        <nav v-if="current.sections.length > 1" class="manual-toc" aria-label="On this tab">
          <a v-for="s in current.sections" :key="s[0]" :href="'#/manual'" @click.prevent="go(s[0])">{{ s[1] }}</a>
        </nav>
        <article class="manual-body" :class="{ 'manual-wide': current.sections.length < 2 }">

          <!-- Getting started -->
          <template v-if="tab === 'start'">
            <section id="m-welcome">
              <h2>What SetupMan does</h2>
              <p>SetupMan keeps every setup you have, with every change you ever made, and helps you choose, compare and tidy them. It records your laps while you drive, works out fuel and pit stops, and analyses ACC's telemetry.</p>
              <p>It runs in the background, with an icon next to the clock: click it and choose <b>Open</b>. It finds your games and their setups folders by itself; if it can't, set the folder in <router-link to="/settings?tab=games">Settings → Games</router-link>.</p>
              <p>SetupMan only <b>reads</b> your setups until you enable folder management (see <a href="#/manual" @click.prevent="go('folder')">Choosing what the game shows</a>). It keeps its own copy of every setup and every change, so nothing gets lost.</p>
            </section>
            <section id="m-games">
              <h2>Supported games</h2>
              <ul>
                <li><b>Assetto Corsa Competizione (ACC)</b>: everything in this manual.</li>
                <li><b>Assetto Corsa (AC)</b>: setups (.ini files) are read, compared and kept with their history; with folder management, SetupMan controls which setups AC lists. Values can be changed, and laps are recorded with pit stop advice (see <a href="#/manual" @click.prevent="go('recognition')">Which setup you drive</a> for what AC reports). Car and track names come from the installed game.</li>
                <li><b>Assetto Corsa EVO (AC EVO)</b>: setups (.carsetup files) are read, compared and kept with their history; with folder management, SetupMan controls which setups AC EVO lists. The file does not name its settings, so some setting names are SetupMan's best reading and may change.</li>
                <li><b>Assetto Corsa Rally (AC Rally)</b>: setups are read, compared and kept with their history. AC Rally keeps all setups in one save file, so SetupMan never changes, renames or removes them; do that in the game.</li>
              </ul>
              <p>Settings → <router-link to="/settings?tab=about">About</router-link> has a table of the features per game.</p>
            </section>
            <section id="m-find">
              <h2>Finding a setup</h2>
              <p>Click the game, then the car, then the track. The track page lists every setup for that car and track: click a column title to sort, and a name to open the setup.</p>
            </section>
          </template>

          <!-- Setups -->
          <template v-else-if="tab === 'setups'">
            <section id="m-values">
              <h2>Seeing and changing values</h2>
              <p>The <b>Setup</b> tab of a setup shows its values like the game's own setup screens: for ACC the tabs Tyres, Electronics, Fuel &amp; strategy, Mechanical grip, Dampers and Aero, with the game's names, and per-wheel settings side by side (left front, right front, left rear, right rear).</p>
              <p>Values are shown as the game shows them: camber and toe in degrees, ride height in mm, wheel rates in N/m, brake bias in %, caster from the car's own table, and so on. For ACC, SetupMan knows these numbers for every car (from the open-source Race Element project). For AC, it reads them from the car's data in the installed game, so on a PC without AC they stay as stored in the file. AC EVO stores real values already. Settings the game itself shows as steps (dampers, for example) stay in steps (clicks).</p>
              <p>Settings the game doesn't show, such as the car name and values it works out itself, are hidden; enable <b>Also show hidden settings</b> to see them. ACC ride heights can differ a few mm from the game, because ACC adjusts them per track (Balance of Performance) and those numbers are not published.</p>
              <h3>Changing values</h3>
              <p>For ACC and AC, click <b>Change values</b>, use − and +, then <b>Save as new version</b> (the setup changes, the old version is kept) or <b>Save as new setup</b> (a copy with a new name). With folder management enabled, the game receives the change immediately. AC stores some settings below zero (camber, for example); those can be changed below zero too. SetupMan does not know each car's limits, so check a changed value on the game's setup screen.</p>
            </section>
            <section id="m-history">
              <h2>Versions and families</h2>
              <p>Every time a setup changes, in the game or in SetupMan, a new version is kept. The <b>History</b> tab lists them, newest first, with a short summary such as “Brake bias +2, Rear wing −1”. <b>Show changes</b> lists the full difference from the previous version, or compares any two versions.</p>
              <p>SetupMan also works out which setup was made from which: a <b>family</b>. The track page's <b>Families</b> tab shows every family as a tree. For setups you had before SetupMan, this is a guess from file dates and how alike the setups are; correct it on the setup's History tab.</p>
            </section>
            <section id="m-compare">
              <h2>Comparing setups</h2>
              <p>On a track page, select two or more setups and open the <b>Compare</b> tab. Settings that differ are highlighted. Setups hidden from the game can be compared too (their newest version). Clicking anywhere on a row selects it. The <b>Similarity map</b> tab shows how alike every pair is, as a percentage of settings that are the same; click a square to compare those two.</p>
              <p>When laps were recorded with the compared setups, the comparison starts with their best and average lap and the track temperature they were driven at; the fastest are marked. Comparing two versions on a setup's History tab does the same per version, so you see what a change was worth.</p>
              <p>Some settings are <b>ignored</b> when comparing, such as the car name, the tyre set, telemetry laps and ACC's fuel-per-lap estimate. They are shown in grey.</p>
            </section>
            <section id="m-duplicates">
              <h2>Duplicates</h2>
              <p><b>Duplicates</b> are setups that are identical, or at least 95% the same (near-duplicates; the percentage is set in Settings). <b>Remove duplicates…</b> on a track page, or on the game page for all tracks, lists every duplicate set with one setup selected to keep: preferably a name without a copy mark such as “(2)”, then the name that follows the naming standard best, then the newest. The others go to the bin.</p>
            </section>
            <section id="m-folder">
              <h2>Choosing what the game shows</h2>
              <p>A game lists every setup in its folder, which gets long. With <b>Folder management</b> enabled in Settings (ACC, AC and AC EVO), SetupMan controls which setups the game lists. Before it changes anything, SetupMan backs up the whole folder and checks that every setup is stored.</p>
              <p>Each setup is then either <b>shown</b> in the game or <b>hidden</b> from it. Hidden setups are not deleted: SetupMan keeps them, and <b>Show in …</b> puts them back. On a track page you can select setups and choose <b>Show only these</b>.</p>
              <p>New setups you save in the game appear on the track page as <b>new setups</b>: keep them, or hide them. Nothing changes while you are driving; it waits until you stop.</p>
            </section>
            <section id="m-names">
              <h2>Names</h2>
              <p>SetupMan reads details from setup names: initials, Q or R, temperature, game version, lap time and your own codes. Correct them on the <b>Notes &amp; details</b> tab.</p>
              <h3>Temperatures in names</h3>
              <p>People write temperatures differently. By default two numbers are the air and the track temperature as the game shows them (3040 = air 30 °C, track 40 °C), one number (23C) is the air temperature, and a dash (23-27C) is a range. In <b>Settings → General → Temperatures in setup names</b> you set this per author: by the initials at the front of the name, or by a word anywhere in it (for example “CDA” in “R 2327 1.10.4 CDA M LFM”). The list shows example names with how they are read, words that recur in names without a known author (select one to add a rule for it) and, where you recorded laps with those setups, which reading fits the measured temperatures.</p>
              <h3>The naming standard</h3>
              <p>SetupMan suggests a name following the naming standard: initials, session, temperature, game version, codes, then the rest of the old name. Click <b>Use this name</b>, or select setups on a track page and choose <b>Apply naming standard</b>. <b>Apply naming standard…</b> on the game page lists every proposed new name for all cars and tracks first; deselect any setup that should keep its name. The track page's <b>Game version</b> column shows the version from the name, or an estimate from the date the setup was last saved (in italics).</p>
              <p>In Settings, SetupMan can name new saves automatically: your initials (Settings → General) unless the name already has initials, the session you saved in (in practice, the session of the setup it was made from), the air and track temperature at that moment as the game shows them, rounded (air 30.7 °C and track 39.1 °C give 3139; outside a session, those of the setup it was made from), and the game version. The game still knows such a setup by its old name: when you save it again under that name, SetupMan stores the save as a new version of the renamed setup and updates the renamed file, so no second copy appears.</p>
            </section>
            <section id="m-temp">
              <h2>Adjusting for temperature</h2>
              <p>On a setup, choose <b>More → Adjust for another temperature</b>. SetupMan looks at your own setups of that car with a temperature in their name, and copies how you changed tyre pressures and brake ducts between them. It shows the proposed changes and what they are based on. Nothing is saved unless you choose to; the result is a new setup.</p>
            </section>
            <section id="m-share">
              <h2>Sharing, importing and downloading</h2>
              <p><b>Share</b> (on a setup, under More) makes one <b>.setupman</b> file with the setup and, if you want, your notes and lap times. The other person opens it on SetupMan's start page or drops it on the page.</p>
              <p><b>Download</b> gives the game's own setup files in a .zip, without SetupMan's notes, for use in the game or other tools. For AC Rally it gives the settings as text.</p>
              <p>To add setups, drop files anywhere on a track page: the game's setup files, ZIP files or shared setups. They are added to SetupMan; use <b>Show in …</b> to use them in the game.</p>
            </section>
          </template>

          <!-- Driving and racing -->
          <template v-else-if="tab === 'racing'">
            <section id="m-laps">
              <h2>Lap times and sessions</h2>
              <p>While ACC or AC runs, SetupMan records your laps: lap time, whether it was valid, fuel used, air and track temperature and hot tyre pressures.</p>
              <p>The <b>Lap times</b> tab of a setup shows your best and average lap (valid laps without pit stops), fuel per lap, and a graph of lap time against track temperature. Click a session in that list for the <b>session report</b>: a summary (best, average, consistency, fuel per lap, temperatures, tyre pressures), the stints between pit stops, and every lap with its time, gap to the best lap, fuel used, temperatures and hot tyre pressures.</p>
            </section>
            <section id="m-recognition">
              <h2>Which setup you drive</h2>
              <p>SetupMan works out which setup you are using already in the pit box, before your first lap, and looks again when you leave the pits. ACC does not say which setup is loaded (it keeps it in memory only), so SetupMan compares what the car reports with your setups for that car and track: the fuel, the pit menu's tyre pressures (ACC takes them from the loaded setup's pit-stop strategy), TC, ABS and engine map and, when setups are equal in all of those, the tyre pressures while the tyres are still cold.</p>
              <p>When several setups still fit equally well, <b>Manual setup select</b> appears under the top bar with those setups as buttons: one click sets it. Leave the pit strategy as the setup has it until you are out of the pits, or SetupMan loses that clue. If it cannot tell, the session appears under <b>Unassigned sessions</b> on the track page, where you choose the setup.</p>
              <p>While a session is recorded, the bar under the top bar always shows the setup laps are stored with; <b>Change setup…</b> corrects it. On track the choice covers the current stint (since you left the pits); in the pits it applies from then on. A setup you chose stays until you load another one: when you leave the pits with different setup values (the pit menu's pressures and fuel come from the setup), SetupMan recognises the new setup and the next laps go to it. In a race the setup stays after a pit stop. Leaving the server ends the session; the game can stay open.</p>
              <p>Laps of two setups in one session? Open the session (track page → Sessions, or Lap times) and choose each stint's setup under <b>Stints</b>; its laps move along.</p>
              <p><b>AC</b> reports less than ACC. SetupMan also uses the setup file the game opened last; this needs Windows to record when files are opened (Settings → Games → Setup recognition shows the status). AC does not say whether a lap was valid (every lap counts as valid), and when the opened file doesn't decide, SetupMan compares only the fuel, so with several setups on the same fuel you may need to choose it yourself. Setups in AC's <i>generic</i> (Any track) folder are not recognised automatically.</p>
            </section>
            <section id="m-prep">
              <h2>Race prep</h2>
              <p><b>Race prep</b> (top bar, or the button on a track page) answers “which setup tonight?”. Choose car, track, session, its length and the expected temperature, and whether that is the air or the track temperature. SetupMan ranks your setups: first the ones made for that session, then the ones whose name has that temperature (e.g. 3040 = air 30 °C, track 40 °C) or comes closest, then the ones with your fastest recorded laps at a similar temperature (within 3 °C). Each setup says why it is ranked there.</p>
              <p>For the chosen setup it shows the fuel plan, from its own recorded laps where possible, with safety margin and tank size from the fuel calculator. The time lost per pit stop is measured from your own recorded stops on that track (cars of the same class first, else any car): drive one full stop with SetupMan running and it is known. Until then the fuel calculator's value is used. With folder management enabled, <b>Show in the game</b> or <b>Show only this one</b> puts it in the game's setup list in one click. Your choices are remembered, also for other devices.</p>
            </section>
            <section id="m-lfm">
              <h2>Low Fuel Motorsport</h2>
              <p>With your LFM ID in Settings → General, Race prep shows your next LFM race: track, start time, session lengths, mandatory stops and the server's weather settings exactly as LFM sets them (for ACC: air temperature, cloud level, rain and randomness). These are settings, not a forecast; the link to the ACC weather simulator (Lenny's Lounge) shows what weather they can give.</p>
              <p>Any other race can be shown by its race number; <b>Next race in series</b> replaces a race with the one after it, and <b>Remove</b> takes a race off the list. A race you signed off from on LFM is no longer shown; select <b>Refresh</b> after signing up or off. <b>Prepare for this race</b> fills in Race prep: your car from the entry list, the track, the race length, the mandatory stops and, when Race prep is set to air temperature, the server's temperature.</p>
            </section>
            <section id="m-fuel">
              <h2>Fuel calculator</h2>
              <p>Open it from the top bar, or from a track page to remember the numbers for that car and track and to use your recorded laps.</p>
              <p><b>Race:</b> race length, average lap time and fuel per lap, plus any mandatory stops, the time a stop costs, a safety margin and the tank size. The <b>pit window</b> is the time in the race when a stop is allowed, in minutes, as the race rules say. The answer is the fuel to start with and to add at each stop, in whole litres, rounded up.</p>
              <p><b>Qualifying:</b> under 12 minutes is one run; longer sessions are two runs, each with an out-lap. It uses your best lap.</p>
            </section>
            <section id="m-pit">
              <h2>Pit stop advice</h2>
              <p>During an ACC or AC session, the <b>Pit stop</b> page in the top bar shows what to set in the pit menu for the next stop. It fits one phone screen without scrolling: fuel to add, the pressures to set per tyre and the main facts; <b>How this is worked out</b> explains the numbers. A value shown in green is already set in the pit menu. It updates by itself, so it can stay open on a phone, tablet or second screen (see <a href="#/manual" @click.prevent="go('network')">Other devices</a>). SetupMan does not change the pit menu itself; enter the values in the game.</p>
              <p><b>Fuel to add:</b> the average fuel use and lap time of your last 5 valid laps (out-laps and in-laps not counted) give the laps still to go and the fuel needed to finish, including the safety margin from the fuel calculator (2 L unless set otherwise). The advice is marked provisional until 3 valid laps are recorded.</p>
              <p><b>Stint plan:</b> nothing needs to be chosen during the race. With a race (R) setup the fuel is split evenly over the stops still needed. With an Endurance setup every stint is as long as the tank and the server's maximum stint time allow, and the last two stints are equal. The tank size and the maximum stint time come from ACC. Extra stops are planned when one stop can't add enough fuel.</p>
              <p><b>Tyre pressures:</b> the average hot pressure of each tyre over the last 3 laps is compared with the target (27.0 psi on dry tyres, 30.0 psi on wets; Settings → Games). The difference is added to the pressure now set in the pit menu.</p>
              <p><b>AC</b> does not report its pit menu, mandatory stops, maximum stint time or rain tyres. The page then shows the pressure change per tyre instead of the value to set, and plans only the stops that fuel requires. Set the target pressures for the AC car you race in Settings → Games.</p>
            </section>
            <section id="m-weekend">
              <h2>A race weekend, step by step</h2>
              <ol class="manual-steps">
                <li><b>Before practice:</b> check that the extra telemetry log is enabled (Settings → Games → ACC); for an LFM race, have your LFM ID in Settings → General.</li>
                <li><b>Practice</b> in the setup you plan to race. If <b>Manual setup select</b> appears, click the right setup once. Drive at least 5 clean laps on race fuel.</li>
                <li><b>Race prep:</b> choose the race and its temperature; it ranks your setups and shows the fuel plan from your practice laps. For another temperature, use <b>More → Adjust for another temperature</b> on the setup.</li>
                <li><b>Fuel calculator</b> (from the track page): race length, mandatory stops, pit loss (filled in from your recorded stops when the box is empty) and safety margin; the fuel to start with and to add per stop.</li>
                <li><b>During the race:</b> keep <b>Pit stop</b> open on a phone or tablet; it shows the fuel to add and the pressures to set before each stop.</li>
                <li><b>Afterwards:</b> under Telemetry, open the race session for its laps and <b>Show brake wear and temperatures</b>, and compare laps in the lap analysis.</li>
              </ol>
            </section>
          </template>

          <!-- Telemetry -->
          <template v-else-if="tab === 'telemetry'">
            <section id="m-tele-files">
              <h2>Where it comes from</h2>
              <p>ACC writes MoTeC telemetry files (Documents\\Assetto Corsa Competizione\\MoTeC). SetupMan reads them and never changes, moves or deletes them; Settings → Games → ACC shows the folder and lets you choose another. New files are read in the background, never while you drive.</p>
              <p>ACC keeps one MoTeC file for as long as you stay on a server, so practice, qualifying and race can be in one file. The Sessions tab shows each session you drove as its own entry, with its own laps, best lap and setup.</p>
              <p>ACC keeps telemetry only for as many laps as the setup's <b>Telemetry laps</b> value (Electronics, at most 99). With folder management enabled, Settings → Games → ACC → <b>Set telemetry laps to 99…</b> lists the setups in the folder that have fewer and sets 99 in the ones you select, after a backup of the folder. Nothing else in the files changes and the setups get no new version.</p>
              <p>ACC saves a session again and again while you drive, and each save keeps the full data (speed, pedals, tyres …) of its last laps only. SetupMan puts the saves of a session together and shows every lap once, taking its data from whichever save still has it. A lap no save has data for keeps its lap time and is marked <b>No data</b>; it can't be analysed.</p>
              <p>Laps that are not full laps (out-lap, in-lap, a restart, a cut or being sent back to the pits) are greyed out and not counted. Laps through the pit lane are full laps.</p>
            </section>
            <section id="m-tele-find">
              <h2>Finding a lap</h2>
              <p><b>Telemetry</b> in the top bar starts with your six latest sessions, then every track you drove, each with the circuit's shape, the cars you drove there, sessions, full laps and your fastest lap. Find a track or car with the search box, show only the tracks of one car, or sort by name, most recent or most laps. Colours follow a timing screen: <b>purple</b> is the fastest lap, <b>green</b> is gaining time, <b>red</b> is losing time.</p>
              <p>A track page starts with the <b>cars</b> you drove there, each with its fastest lap. Choose one or more cars to narrow everything below; choose two or more and select <b>Compare fastest laps</b> to put their fastest laps side by side, even cars of different classes. Three tabs then find a lap:</p>
              <ul>
                <li><b>Best laps</b>: every full lap, fastest first, filtered by setup, session type and period, leaving out invalid and pit-lane laps when SetupMan recorded them.</li>
                <li><b>Setups</b>: every setup driven there (per car) with its fastest lap, the gap to the fastest setup and the average of its best five laps; tick two or more and select <b>Compare fastest laps</b>.</li>
                <li><b>Sessions</b>: the sessions by day (<b>Go to</b> jumps to a month); open one for every lap, its note and the setup you drove. Sessions SetupMan recorded get their setup automatically (and change along when you correct it with <b>Wrong setup?</b>); for older sessions, choose the setup in the list.</li>
              </ul>
              <p>The star makes a lap the <b>reference lap</b> for that car on that track; <b>Fastest vs reference</b> compares the two in one click.</p>
            </section>
            <section id="m-tele-analysis">
              <h2>Lap analysis</h2>
              <p>Tick up to four laps anywhere and select <b>Compare laps</b>. The lap analysis puts them on top of each other along the distance from the line, each stretched to the length of the first lap (blue); the others are orange, teal and pink. Laps are named “Lap n” within one session, by their setup when you compare setups, otherwise by their day; by their car when the cars differ.</p>
              <ul>
                <li><b>Result</b>: the difference over the lap, where the second lap lost and gained the most (click to zoom in), top speed, full throttle and time on the brakes of every lap.</li>
                <li><b>Time difference</b> to the first lap: shaded red where the second lap loses time, green where it gains. <b>Compare against this</b> makes another lap the first.</li>
                <li><b>Graphs</b> of any channel: Driver, Tyres, Brakes and Suspension are ready-made sets; <b>Channels</b> picks any channel the file has, and <b>Custom</b> remembers your own set. Point at a graph to read every lap at that point; drag to zoom in, double-click for the whole lap, Ctrl + wheel zooms and Shift + wheel moves along the lap. With the keyboard: the arrow keys move the cursor, + and − zoom, 0 shows the whole lap.</li>
                <li><b>Track map</b> with the turn numbers: green where the second lap gains time, red where it loses; click the map to move the cursor there.</li>
                <li><b>Car</b>: tyre pressure, tyre temperature and brake temperature per wheel, coloured from cool to hot: the lap average, or the value at the cursor once you point at a graph.</li>
                <li><b>Turns</b>: the time gained or lost in every turn with a bar, and why, e.g. “Braking 12 m later · Minimum speed +4 km/h · Exit speed +3 km/h” (only reasons that explain the gain or loss). Click a turn to zoom in.</li>
              </ul>
            </section>
            <section id="m-tele-turns">
              <h2>Turn numbers</h2>
              <p>Turns use the circuit's official numbers, taken from Wikimedia Commons track maps, so T5 is the T5 you know. Point at a turn number in the turns table or on the track map for its name, where known. For a track without official numbers yet, SetupMan counts the track's slow points itself, and the Turns card says so.</p>
            </section>
            <section id="m-tele-brakes">
              <h2>Brakes over a session</h2>
              <p><b>Show brake wear and temperatures</b> in an opened session on a track page charts each wheel's pad life and brake temperature lap by lap over the whole session, with the pad and disc used per lap and per hour: made for long stints. Wear comes from the extra telemetry log; without it, brake temperatures come from ACC's own files for the laps they kept.</p>
            </section>
            <section id="m-tele-extra">
              <h2>Extra telemetry log</h2>
              <p>MoTeC files have no car position, fuel, lap validity or brake wear. While you drive, SetupMan records those and more (slip angle and ratio, brake pressure, temperature and wear, ride height, pitch and roll) about 10 times per second in its own folder: a few MB per hour, included in full backups. With it, the track map follows the car's real position, brakes over a session shows the wear, and the extra channels appear under <b>Channels</b>, marked “extra”. It can be disabled in Settings → Games → ACC.</p>
            </section>
          </template>

          <!-- Backups -->
          <template v-else-if="tab === 'backups'">
            <section id="m-backup-kinds">
              <h2>Two kinds of backup</h2>
              <p>A <b>setups folder backup</b> is a copy of a game's setups folder; restoring it makes the game's current setups folder exactly as it was. A backup made of another folder (on another PC, or before the setups folder was changed in Settings) is restored into the current setups folder; the confirmation says so.</p>
              <p>A <b>full SetupMan backup</b> holds everything: all versions, history, notes, lap times, settings, telemetry notes and reference laps, and SetupMan's extra telemetry logs. The game's own telemetry files are not included (SetupMan never changes them). Download one to keep a copy on another drive or PC.</p>
              <p>Before every restore, SetupMan backs up the current state, so a restore can be undone.</p>
            </section>
            <section id="m-backup-where">
              <h2>Where backups are</h2>
              <p>Backups are saved in <code>%APPDATA%\\SetupMan\\backups</code> (setups folder backups, per game) and <code>%APPDATA%\\SetupMan\\full-backups</code>; <b>Open folder</b> shows them in Explorer. To use a full backup from another PC or drive, choose <b>Restore from a file…</b>.</p>
            </section>
            <section id="m-log">
              <h2>Log file</h2>
              <p>SetupMan writes what it does to <code>%APPDATA%\\SetupMan\\setupman.log</code>: when a game starts and stops, when you start and stop driving, every recorded lap with what the game reported, which setup it recognised, and what changed in the setups folder. Type that path in Windows' Run box (Windows key + R) to open it. When something doesn't work as expected, this file shows what SetupMan saw.</p>
            </section>
          </template>

          <!-- Other devices -->
          <template v-else-if="tab === 'devices'">
            <section id="m-network">
              <h2>Phone, tablet and other PCs</h2>
              <p>In <router-link to="/settings?tab=network">Settings → Network</router-link>, enable <b>Network access</b>. The tab then shows the address to open on the other device (for example http://192.168.1.20:8765) and a 6-digit PIN. Type the PIN once on each device; after that it has full use of SetupMan. <b>Generate new PIN</b> locks out every other device that used the old one.</p>
              <p>On a network where you trust every device, such as your own PC and phone at home, <b>Require PIN</b> can be disabled: every device on the network then has full use of SetupMan without a PIN. Enabled is recommended.</p>
            </section>
            <section id="m-lan">
              <h2>setupman.lan instead of an IP address</h2>
              <p>With a reverse proxy such as <b>Nginx Proxy Manager</b> (NPM) on your network, every device can open SetupMan at <b>http://setupman.lan</b>, without an IP address or port number. SetupMan does not need to run on the same computer as NPM.</p>
              <ol class="manual-steps">
                <li><b>Give the PC a fixed address.</b> In your router, reserve the PC's current IP address (often called <i>DHCP reservation</i> or <i>static lease</i>), so it stays the same after a restart.</li>
                <li><b>Enable network access in SetupMan.</b> <router-link to="/settings?tab=network">Settings → Network</router-link> shows the PC's address and port, for example 192.168.1.20 and 8765.</li>
                <li><b>Point the name at NPM.</b> setupman.lan must lead to the computer that runs NPM (not the SetupMan PC, unless NPM runs there). Add a local DNS record in your router, Pi-hole (<i>Local DNS → DNS Records</i>) or AdGuard Home (<i>Filters → DNS rewrites</i>): <code>setupman.lan</code> → the IP address of the NPM computer. Without a local DNS server, add the line <code>192.168.1.10 setupman.lan</code> (the NPM computer's address) to the hosts file of each device instead; on Windows that is <code>C:\\Windows\\System32\\drivers\\etc\\hosts</code>. Avoid names ending in <i>.local</i>: those are reserved for automatic device discovery and often fail.</li>
                <li><b>Add a proxy host in NPM.</b> <i>Hosts → Proxy Hosts → Add Proxy Host</i>. Domain Names: <code>setupman.lan</code>. Scheme: <code>http</code>. Forward Hostname / IP: the SetupMan PC's address (192.168.1.20 in the example; if NPM runs in Docker on the same PC, use <code>host.docker.internal</code>). Forward Port: SetupMan's port (8765). Leave the SSL tab at <i>None</i>: certificates from Let's Encrypt are not available for .lan names. Save.</li>
                <li><b>Enter the name in SetupMan.</b> Settings → Network → <b>Network name</b>: <code>setupman.lan</code>, then <b>Save</b>. SetupMan refuses names it does not know, which protects it against malicious web pages.</li>
                <li><b>Open http://setupman.lan</b> on any device and type the PIN once. Live pages such as Pit stop update through the proxy without further settings.</li>
              </ol>
            </section>
            <section id="m-lan-trouble">
              <h2>If it does not work</h2>
              <ul>
                <li>“Use the IP address to open SetupMan from another computer”: the network name in SetupMan is empty or different from the name in the address bar.</li>
                <li>“SetupMan only accepts connections from its own PC”: network access is disabled.</li>
                <li>NPM shows <i>502 Bad Gateway</i>: it cannot reach SetupMan. Check the forward address and port, and allow SetupMan through Windows Firewall for private networks. If you change SetupMan's port, change the Forward Port in NPM as well.</li>
                <li><b>Works on one device but not on another</b> (often a phone or tablet): that device does not ask your DNS server for the name. Most devices get their DNS server from the router, so set the router to hand out your DNS server (on a Fritz!Box: <i>Home Network → Network → Network Settings</i>, <b>Local DNS server</b> under both IPv4 and IPv6). On the phone, set Android's <b>Private DNS</b> to Automatic, or on an iPhone turn off <b>Limit IP Address Tracking</b> and <b>iCloud Private Relay</b>. Then turn Wi-Fi off and on.</li>
              </ul>
            </section>
          </template>

          <!-- Words -->
          <template v-else>
            <section id="m-words">
              <h2>Words used in SetupMan</h2>
              <dl class="manual-words">
                <dt>Version</dt><dd>One saved state of a setup. Every change makes a new version.</dd>
                <dt>Family</dt><dd>Setups made from each other, shown as a tree.</dd>
                <dt>Duplicates</dt><dd>Setups that are identical, or at least 95% the same (near-duplicates).</dd>
                <dt>Clicks</dt><dd>The game's steps for a setting, as stored in the setup file.</dd>
                <dt>Shown / hidden</dt><dd>Whether the setup is in the game's setups folder. Hidden setups are kept by SetupMan.</dd>
                <dt>Ignored settings</dt><dd>Settings left out when SetupMan decides how alike setups are (shown in grey).</dd>
                <dt>Hidden settings</dt><dd>Values in the setup file that the game doesn't show on its setup screens.</dd>
                <dt>Full lap</dt><dd>A lap that starts and ends at the line and is as long as the track (laps through the pit lane included); only full laps count in lap totals and best laps.</dd>
                <dt>No data</dt><dd>A telemetry lap ACC kept only the lap time of: it can't be analysed.</dd>
                <dt>Reference lap</dt><dd>The lap marked with a star for a car on a track, to compare others against.</dd>
                <dt>Extra telemetry log</dt><dd>SetupMan's own recording while you drive: car position, fuel, lap validity, brake wear and more.</dd>
              </dl>
            </section>
          </template>
        </article>
      </div>
    </main>`,
};
