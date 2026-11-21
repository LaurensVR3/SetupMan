// The SetupMan manual: everything the app does, in plain words, with a contents list.
import { setCrumbs } from "../components/common.js";

const SECTIONS = [
  ["start", "Getting started"],
  ["find", "Finding a setup"],
  ["values", "Seeing and changing values"],
  ["history", "Versions and history"],
  ["compare", "Comparing setups"],
  ["games", "Supported games"],
  ["acc", "Choosing what the game shows"],
  ["laps", "Lap times"],
  ["prep", "Race prep"],
  ["fuel", "Fuel calculator"],
  ["pit", "Pit stop advice"],
  ["temp", "Adjusting for temperature"],
  ["names", "Names"],
  ["share", "Sharing, importing and downloading"],
  ["backups", "Backups"],
  ["network", "Other devices and setupman.lan"],
  ["words", "Words used in SetupMan"],
];

export const ManualPage = {
  setup() {
    setCrumbs([{ label: "Manual" }]);
    const go = (id) => document.getElementById("m-" + id)?.scrollIntoView({ behavior: "smooth", block: "start" });
    return { SECTIONS, go };
  },
  template: `
    <main class="page manual">
      <h1 class="page-title">Manual</h1>
      <p class="page-sub">How SetupMan works, from finding a setup to backups.</p>
      <div class="manual-layout">
        <nav class="manual-toc" aria-label="Contents">
          <a v-for="s in SECTIONS" :key="s[0]" :href="'#/manual'" @click.prevent="go(s[0])">{{ s[1] }}</a>
        </nav>
        <article class="manual-body">
          <section id="m-start">
            <h2>Getting started</h2>
            <p>SetupMan runs in the background, with an icon next to the clock. Click it and choose <b>Open</b> to see this page. It finds your games and their setups folders by itself. If it can't, set the folder in <router-link to="/settings?tab=games">Settings</router-link>.</p>
            <p>SetupMan only <b>reads</b> your setups until folder management is enabled (see <a href="#/manual" @click.prevent="go('acc')">Choosing what the game shows</a>). It keeps its own copy of every setup and every change, so nothing gets lost.</p>
          </section>
          <section id="m-games">
            <h2>Supported games</h2>
            <ul>
              <li><b>Assetto Corsa Competizione (ACC)</b>: everything in this manual.</li>
              <li><b>Assetto Corsa (AC)</b>: setups (.ini files) are read, compared and kept with their history; with folder management, SetupMan controls which setups AC lists. Values can be changed, and laps are recorded with pit stop advice (see Lap times for what AC reports). Car and track names come from the installed game.</li>
              <li><b>Assetto Corsa EVO (AC EVO)</b>: setups (.carsetup files) are read, compared and kept with their history; with folder management, SetupMan controls which setups AC EVO lists. The file does not name its settings, so some setting names are SetupMan's best reading and may change.</li>
              <li><b>Assetto Corsa Rally (AC Rally)</b>: setups are read, compared and kept with their history. AC Rally keeps all setups in one save file, so SetupMan never changes, renames or removes them; do that in the game.</li>
            </ul>
            <p>Settings → <router-link to="/settings?tab=about">About</router-link> has a table of the features per game.</p>
          </section>
          <section id="m-find">
            <h2>Finding a setup</h2>
            <p>Click the game, then the car, then the track. The track page lists all setups for that car and track. Click a column title to sort, and click a name to open the setup.</p>
          </section>
          <section id="m-values">
            <h2>Seeing and changing values</h2>
            <p>The <b>Setup</b> tab of a setup shows every value, grouped like the game's setup screens. Values are shown as the game shows them: camber and toe in degrees, ride height in mm, wheel rates in N/m, brake bias in %, caster from the car's own table, and so on. For ACC, SetupMan knows these numbers for every car (taken from the open-source Race Element project). For AC, it reads them from the car's data in the installed game, so on a PC without AC the numbers stay as stored in the file. AC EVO stores real values already. Settings the game itself shows as steps (dampers, for example) stay in steps (clicks).</p>
            <p>For ACC and AC, click <b>Change values</b>, use − and +, then <b>Save as new version</b> (the setup changes, the old version is kept) or <b>Save as new setup</b> (a copy with a new name). With folder management enabled, the game receives the change immediately. AC stores some settings below zero (camber, for example); those can be changed below zero too. SetupMan does not know each car's limits, so check a changed value on the game's setup screen.</p>
          </section>
          <section id="m-history">
            <h2>Versions and history</h2>
            <p>Every time a setup changes, in the game or in SetupMan, a new version is kept. The <b>History</b> tab lists them, newest first, with a short summary such as “Brake bias +2, Rear wing −1”. <b>Show changes</b> lists the full difference from the previous version, or compare any two versions.</p>
            <p>SetupMan also works out which setup was made from which: a <b>family</b>. The track page's <b>Families</b> tab shows every family as a tree. For setups you had before SetupMan, this is a guess from file dates and how alike the setups are; you can correct it on the setup's History tab.</p>
          </section>
          <section id="m-compare">
            <h2>Comparing setups</h2>
            <p>On a track page, select two or more setups and open the <b>Compare</b> tab. Settings that differ are highlighted. Setups hidden from the game can be compared too (their newest version). Clicking anywhere on a row selects it. The <b>Similarity map</b> tab shows how alike every pair is, as a percentage of settings that are the same; click a square to compare those two.</p>
            <p>When laps were recorded with the compared setups, the comparison starts with their best and average lap and the track temperature they were driven at; the fastest are marked. Comparing two versions on a setup&#39;s History tab does the same per version, so you see what a change was worth.</p>
            <p><b>Duplicates</b> are setups that are identical, or at least 95% the same (near-duplicates; the percentage is set in Settings). <b>Remove duplicates…</b> on a track page, or on the game page for all tracks, lists every duplicate set with one setup selected to keep (preferably a name without a copy mark such as “(2)”, then the name that follows the naming standard best, then the newest); the others go to the bin.</p>
            <p>The <b>Setup</b> tab of a setup shows its values like the game's own setup screens: for ACC the tabs Tyres, Electronics, Fuel &amp; strategy, Mechanical grip, Dampers and Aero, with the game's names, and per-wheel settings side by side (left front, right front, left rear, right rear). Settings the game doesn't show, such as the car name and values it works out itself, are hidden; enable <b>Also show hidden settings</b> to see them.</p>
            <p>Some settings are <b>ignored</b> when comparing, such as the car name, the tyre set, telemetry laps and ACC's fuel-per-lap estimate. They are shown in grey.</p>
          </section>
          <section id="m-acc">
            <h2>Choosing what the game shows</h2>
            <p>A game lists every setup in its folder, which gets long. With <b>Folder management</b> enabled in Settings (ACC, AC and AC EVO), SetupMan controls which setups the game lists. Before it changes anything, SetupMan creates a backup of the whole folder and verifies that every setup is stored.</p>
            <p>Then each setup is either <b>shown</b> in the game or <b>hidden</b> from it. Hidden setups are not deleted: SetupMan keeps them, and <b>Show in …</b> puts them back. On a track page you can select setups and choose <b>Show only these</b>.</p>
            <p>New setups you save in the game appear on the track page as <b>new setups</b>: keep them, or hide them. Nothing changes while you are driving; it waits until you stop.</p>
          </section>
          <section id="m-laps">
            <h2>Lap times</h2>
            <p>While ACC or AC runs, SetupMan records your laps: lap time, whether it was valid, fuel used, air and track temperature and hot tyre pressures. It works out which setup you are using already in the pit box, before your first lap, and looks again when you leave the pits. ACC does not say which setup is loaded (it keeps it in memory only), so SetupMan compares what the car reports with your setups for that car and track: the fuel, the pit menu's tyre pressures (ACC takes them from the loaded setup's pit-stop strategy), TC, ABS and engine map, and, when setups are equal in all of those, the tyre pressures while the tyres are still cold. When several setups still fit equally well, <b>Manual setup select</b> appears under the top bar with those setups as buttons: one click sets it. Leave the pit strategy as the setup has it until you are out of the pits, or SetupMan loses that clue. For AC, SetupMan also uses the setup file the game opened last; this needs Windows to record when files are opened (Settings → Games → Setup recognition shows the status). If it cannot determine the setup, the session appears under <b>Unassigned sessions</b> on the track page, where the setup can be selected.</p>
            <p><b>AC</b> reports less than ACC: it does not say whether a lap was valid (every lap counts as valid), and when the opened file doesn't decide, SetupMan compares only the fuel, so with several setups on the same fuel you may need to select it yourself. Setups in AC's <i>generic</i> (Any track) folder are not recognised automatically.</p>
            <p>The <b>Lap times</b> tab of a setup shows your best and average lap (valid laps without pit stops), fuel per lap, and a graph of lap time against track temperature.</p>
            <p>Click a session in that list for the <b>session report</b>: a summary (best, average, consistency, fuel per lap, temperatures, tyre pressures), the stints between pit stops, and every lap with its time, gap to the best lap, fuel used, temperatures and hot tyre pressures.</p>
          </section>
          <section id="m-prep">
            <h2>Race prep</h2>
            <p><b>Race prep</b> (top bar, or the button on a track page) answers "which setup tonight?". Choose car, track, session, its length and the expected temperature, and whether that is the air or the track temperature. SetupMan ranks your setups: first the ones made for that session, then the ones whose name has that temperature (air or track, e.g. 3040 = air 30 °C, track 40 °C) or comes closest, then the ones with your fastest recorded laps at a similar temperature (within 3 °C). Each setup says why it is ranked there.</p>
            <p>For the chosen setup it shows the fuel plan, from its own recorded laps where possible, with pit loss, safety margin and tank size from the fuel calculator. With folder management enabled, <b>Show in the game</b> or <b>Show only this one</b> puts it in the game's setup list in one click. Your choices are remembered, also for other devices.</p>
            <p><b>Low Fuel Motorsport.</b> With your LFM ID in Settings → General, Race prep shows your next LFM race: track, start time, session lengths, mandatory stops and the server's weather settings exactly as LFM sets them (for ACC: air temperature, cloud level, rain and randomness). These are settings, not a forecast; the link to the ACC weather simulator (Lenny's Lounge) shows what weather they can give. Any other race can be shown by its race number; <b>Next race in series</b> replaces a race with the one after it, and <b>Remove</b> takes a race off the list. A race you signed off from on LFM is no longer shown; select <b>Refresh</b> after signing up or off.<b>Prepare for this race</b> fills in Race prep: your car from the entry list, the track, the race length, the mandatory stops and, when Race prep is set to air temperature, the server's temperature.</p>
          </section>
          <section id="m-fuel">
            <h2>Fuel calculator</h2>
            <p>Open it from the top bar, or from a track page to remember the numbers for that car and track and to use your recorded laps.</p>
            <p><b>Race:</b> race length, average lap time and fuel per lap, plus any mandatory stops, the time a stop costs, a safety margin and the tank size. The <b>pit window</b> is the time in the race when a stop is allowed, in minutes, as the race rules say. The answer is the fuel to start with and to add at each stop, in whole litres, rounded up.</p>
            <p><b>Qualifying:</b> under 12 minutes is one run; longer sessions are two runs, each with an out-lap. It uses your best lap.</p>
          </section>
          <section id="m-pit">
            <h2>Pit stop advice</h2>
            <p>During an ACC or AC session, the <b>Pit stop</b> page in the top bar shows what to set in the pit menu for the next stop. It is made to fit one phone screen without scrolling: fuel to add, the pressures to set per tyre and the main facts; <b>How this is worked out</b> explains the numbers. A value shown in green is already set in the pit menu. It updates by itself, so it can stay open on a phone, tablet or second screen (see <a href="#/manual" @click.prevent="go('network')">Using SetupMan from other devices</a>).</p>
            <p><b>Fuel to add:</b> the average fuel use and lap time of your last 5 valid laps (out-laps and in-laps not counted) give the laps still to go and the fuel needed to finish, including the safety margin from the fuel calculator (2 L unless set otherwise). The advice is marked provisional until 3 valid laps are recorded.</p>
            <p><b>Stint plan:</b> nothing needs to be chosen during the race. With a race (R) setup the fuel is split evenly over the stops still needed. With an Endurance setup every stint is as long as the tank and the server's maximum stint time allow, and the last two stints are equal. The tank size and the maximum stint time come from ACC. Extra stops are planned when one stop can't add enough fuel.</p>
            <p><b>Tyre pressures:</b> the average hot pressure of each tyre over the last 3 laps is compared with the target (27.0 psi on dry tyres, 30.0 psi on wets; Settings → Games). The difference is added to the pressure now set in the pit menu.</p>
            <p>SetupMan does not change the pit menu itself; enter the values in the game.</p>
            <p><b>AC</b> does not report its pit menu, mandatory stops, maximum stint time or rain tyres. The page then shows the pressure change per tyre instead of the value to set, and plans only the stops that fuel requires. Set the target pressures for the AC car you race in Settings → Games.</p>
          </section>
          <section id="m-temp">
            <h2>Adjusting for temperature</h2>
            <p>On a setup, choose <b>More → Adjust for another temperature</b>. SetupMan looks at your own setups of that car that have a temperature in their name, and copies how you changed tyre pressures and brake ducts between them. It shows the proposed changes and what they are based on. Nothing is saved unless you choose to; the result is a new setup.</p>
          </section>
          <section id="m-names">
            <h2>Names</h2>
            <p>SetupMan reads details from setup names: initials, Q or R, temperature, game version, lap time and your own codes. You can correct them on the <b>Notes &amp; details</b> tab.</p>
            <p><b>Temperatures</b> are written differently by different people. By default two numbers are the air and the track temperature as the game shows them (3040 = air 30 °C, track 40 °C) and one number (23C) is the air temperature; a dash (23-27C) is a range. In <b>Settings → General → Temperatures in setup names</b> you set this per author: by the initials at the front of the name, or by a word anywhere in it (for example “CDA” in “R 2327 1.10.4 CDA M LFM”). The list shows example names with how they are read, words that recur in names without a known author (select one to add a rule for it), and, where you recorded laps with those setups, which reading fits the measured temperatures.</p>
            <p>It also suggests a name following the naming standard: initials, session, temperature, game version, codes, then the rest of the old name. Click <b>Use this name</b>, or select setups on a track page and choose <b>Apply naming standard</b>. <b>Apply naming standard…</b> on the game page lists every proposed new name for all cars and tracks first; deselect any setup that should keep its name. The track page's <b>Game version</b> column shows the version from the name, or an estimate from the date the setup was last saved (in italics). In Settings, SetupMan can apply the naming standard to new saves automatically: your initials (Settings → General) unless the name already has initials, the session you saved in (in practice, the session of the setup it was made from), the air and track temperature at that moment as the game shows them, rounded (air 30.7 °C and track 39.1 °C give 3139; outside a session, those of the setup it was made from), and the game version. The game still knows such a setup by its old name: when you save it again under that name, SetupMan stores the save as a new version of the renamed setup and updates the renamed file, so no second copy appears.</p>
          </section>
          <section id="m-share">
            <h2>Sharing, importing and downloading</h2>
            <p><b>Share</b> (on a setup, under More) makes one <b>.setupman</b> file with the setup and, if you want, your notes and lap times. The other person opens it on SetupMan's start page or drops it on the page.</p>
            <p><b>Download</b> gives the game's own setup files in a .zip, without SetupMan's notes, for use in the game or other tools. For AC Rally it gives the settings as text.</p>
            <p>To add setups, drop files anywhere on a track page: the game's setup files, ZIP files or shared setups. They are added to SetupMan; use <b>Show in …</b> to use them in the game.</p>
          </section>
          <section id="m-backups">
            <h2>Backups</h2>
            <p>Settings has two kinds of backup. A <b>setups folder backup</b> is a copy of a game's setups folder; restoring it makes the game's current setups folder exactly as it was. A backup made of another folder (on another PC, or before the setups folder was changed in Settings) is restored into the current setups folder; the confirmation says so. A <b>full SetupMan backup</b> holds everything: all versions, history, notes, lap times and settings. Download one to keep a copy on another drive or PC. Before every restore, SetupMan backs up the current state, so a restore can be undone.</p>
            <p>Backups are saved in <code>%APPDATA%\\SetupMan\\backups</code> (setups folder backups, per game) and <code>%APPDATA%\\SetupMan\\full-backups</code>; <b>Open folder</b> shows them in Explorer. To use a full backup from another PC or drive, choose <b>Restore from a file…</b>.</p>
            <p><b>Log file.</b> SetupMan writes what it does to <code>%APPDATA%\\SetupMan\\setupman.log</code>: when a game starts and stops, when you start and stop driving, every recorded lap with what the game reported, which setup it recognised, and what changed in the setups folder. Type that path in Windows' Run box (Windows key + R) to open it. When something doesn't work as expected, this file shows what SetupMan saw.</p>
          </section>
          <section id="m-network">
            <h2>Using SetupMan from other devices</h2>
            <p>In <router-link to="/settings?tab=network">Settings → Network</router-link>, enable <b>Network access</b>. The tab then shows the address to open on the other device (for example http://192.168.1.20:8765) and a 6-digit PIN. Type the PIN once on each device; after that it has full use of SetupMan. <b>Generate new PIN</b> locks out every other device that used the old one. On a network where you trust every device, such as your own PC and phone at home, <b>Require PIN</b> can be disabled: every device on the network then has full use of SetupMan without a PIN. Enabled is recommended.</p>
            <h3>A name instead of an IP address: setupman.lan</h3>
            <p>With a reverse proxy such as <b>Nginx Proxy Manager</b> (NPM) on your network, every device can open SetupMan at <b>http://setupman.lan</b>, without an IP address or port number. SetupMan does not need to run on the same computer as NPM.</p>
            <ol class="manual-steps">
              <li><b>Give the PC a fixed address.</b> In your router, reserve the PC's current IP address (often called <i>DHCP reservation</i> or <i>static lease</i>), so it stays the same after a restart.</li>
              <li><b>Enable network access in SetupMan.</b> <router-link to="/settings?tab=network">Settings → Network</router-link> shows the PC's address and port, for example 192.168.1.20 and 8765.</li>
              <li><b>Point the name at NPM.</b> setupman.lan must lead to the computer that runs NPM (not the SetupMan PC, unless NPM runs there). Add a local DNS record in your router, Pi-hole (<i>Local DNS → DNS Records</i>) or AdGuard Home (<i>Filters → DNS rewrites</i>): <code>setupman.lan</code> → the IP address of the NPM computer. Without a local DNS server, add the line <code>192.168.1.10 setupman.lan</code> (the NPM computer's address) to the hosts file of each device instead; on Windows that is <code>C:\Windows\System32\drivers\etc\hosts</code>. Avoid names ending in <i>.local</i>: those are reserved for automatic device discovery and often fail.</li>
              <li><b>Add a proxy host in NPM.</b> <i>Hosts → Proxy Hosts → Add Proxy Host</i>. Domain Names: <code>setupman.lan</code>. Scheme: <code>http</code>. Forward Hostname / IP: the SetupMan PC's address (192.168.1.20 in the example; if NPM runs in Docker on the same PC, use <code>host.docker.internal</code>). Forward Port: SetupMan's port (8765). Leave the SSL tab at <i>None</i>: certificates from Let's Encrypt are not available for .lan names. Save.</li>
              <li><b>Enter the name in SetupMan.</b> Settings → Network → <b>Network name</b>: <code>setupman.lan</code>, then <b>Save</b>. SetupMan refuses names it does not know, which protects it against malicious web pages.</li>
              <li><b>Open http://setupman.lan</b> on any device and type the PIN once. Live pages such as Pit stop update through the proxy without further settings.</li>
            </ol>
            <p><b>If it does not work:</b> “use the IP address to open SetupMan from another computer” means the network name in SetupMan is empty or different from the name in the address bar. “SetupMan only accepts connections from its own PC” means network access is disabled. NPM showing <i>502 Bad Gateway</i> means it cannot reach SetupMan: check the forward address and port, and allow SetupMan through Windows Firewall for private networks. If you change SetupMan's port, change the Forward Port in NPM as well.</p>
            <p><b>Works on one device but not on another</b> (often a phone or tablet): that device does not ask your DNS server for the name. Most devices get their DNS server from the router, so set the router to hand out your DNS server (on a Fritz!Box: <i>Home Network → Network → Network Settings</i>, <b>Local DNS server</b> under both IPv4 and IPv6). On the phone, set Android's <b>Private DNS</b> to Automatic, or on an iPhone turn off <b>Limit IP Address Tracking</b> and <b>iCloud Private Relay</b>. Then turn Wi-Fi off and on.</p>
          </section>
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
            </dl>
          </section>
        </article>
      </div>
    </main>`,
};
