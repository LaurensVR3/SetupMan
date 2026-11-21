package history

import (
	"errors"
	"sort"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/naming"
	"setupman/internal/setup"
	"setupman/internal/store"
)

// Node is one setup in a family tree.
type Node struct {
	SetupID   int64     `json:"setupId"`
	Name      string    `json:"name"`
	Date      time.Time `json:"date"`
	Session   string    `json:"session"`
	LinkKind  string    `json:"linkKind"`
	Score     float64   `json:"score"`
	InFolder  bool      `json:"inFolder"`
	Versions  int       `json:"versions"`
	ParentSeq int       `json:"parentSeq"` // which version of the parent it came from
	Children  []*Node   `json:"children"`
}

// GroupTrees is the forest of one car on one track.
type GroupTrees struct {
	Car       string  `json:"car"`
	Track     string  `json:"track"`
	CarName   string  `json:"carName"`   // filled in by the library (readable names, D-27)
	TrackName string  `json:"trackName"` // idem
	Roots     []*Node `json:"roots"`
}

// PreviewSummary counts a (proposed) history.
type PreviewSummary struct {
	Families int `json:"families"`
	Linked   int `json:"linked"`
	Longest  int `json:"longest"`
	Changed  int `json:"changed"` // setups whose parent would change
}

// Preview is the result of a history rebuild (D-62).
type Preview struct {
	Summary PreviewSummary `json:"summary"`
	Groups  []GroupTrees   `json:"groups"`
}

// item is a setup with everything needed to build trees.
type item struct {
	s        store.Setup
	versions []store.Version
	first    time.Time
}

type gameData struct {
	items      map[int64]*item
	versionOwn map[int64]int64 // version id → setup id
	groups     map[string][]*item
}

func loadGame(q store.Q, gameID string) (*gameData, error) {
	setups, err := q.SetupsForGame(gameID)
	if err != nil {
		return nil, err
	}
	g := &gameData{items: map[int64]*item{}, versionOwn: map[int64]int64{}, groups: map[string][]*item{}}
	for _, s := range setups {
		vs, err := q.Versions(s.ID)
		if err != nil {
			return nil, err
		}
		it := &item{s: s, versions: vs}
		if len(vs) > 0 {
			it.first = vs[0].FileModified
		}
		for _, v := range vs {
			g.versionOwn[v.ID] = s.ID
		}
		g.items[s.ID] = it
		k := groupKey(s.Car, s.Track)
		g.groups[k] = append(g.groups[k], it)
	}
	for _, list := range g.groups {
		sort.SliceStable(list, func(i, j int) bool {
			if !list[i].first.Equal(list[j].first) {
				return list[i].first.Before(list[j].first)
			}
			return list[i].s.ID < list[j].s.ID
		})
	}
	return g, nil
}

// parentSetup returns the setup id a setup came from (0 = none).
func (g *gameData) parentSetup(parentVersion map[int64]int64, id int64) int64 {
	return g.versionOwn[parentVersion[id]]
}

// isAncestor reports whether anc is an ancestor of id (or id itself).
func (g *gameData) isAncestor(parentVersion map[int64]int64, anc, id int64) bool {
	for steps := 0; id != 0 && steps < 10000; steps++ {
		if id == anc {
			return true
		}
		id = g.parentSetup(parentVersion, id)
	}
	return false
}

// Rebuild recalculates probable links (only setups whose link is probable or none; observed
// and manual links are kept). With apply=false nothing is saved (preview).
func Rebuild(st *store.Store, gameID string, parse Parser, cfg Config, apply bool) (Preview, error) {
	var out Preview
	err := st.Tx(func(q store.Q) error {
		g, err := loadGame(q, gameID)
		if err != nil {
			return err
		}
		cache := &fieldCache{q: q, parse: parse, byID: map[int64][]setup.Field{}}
		parentVersion := map[int64]int64{}
		kind := map[int64]string{}
		score := map[int64]float64{}
		for id, it := range g.items {
			parentVersion[id], kind[id], score[id] = it.s.ParentVersionID, it.s.LinkKind, it.s.LinkScore
		}

		for _, list := range g.groups {
			for i, it := range list {
				if it.s.LinkKind == store.LinkObserved || it.s.LinkKind == store.LinkManual || len(it.versions) == 0 {
					continue
				}
				mine := cache.fields(&it.versions[0])
				parentVersion[it.s.ID], kind[it.s.ID], score[it.s.ID] = 0, store.LinkNone, 0
				best := 0.0
				for _, cand := range list[:i] { // only older setups
					cv := versionAt(cand.versions, it.first)
					if cv == nil || g.isAncestor(parentVersion, it.s.ID, cand.s.ID) {
						continue
					}
					sc := compare.Compare(cache.fields(cv), mine).Score
					if linkOK(sc, cand.s.Name, it.s.Name, cfg) && sc > best {
						best = sc
						parentVersion[it.s.ID], kind[it.s.ID], score[it.s.ID] = cv.ID, store.LinkProbable, sc
					}
				}
			}
		}

		for id, it := range g.items {
			if parentVersion[id] != it.s.ParentVersionID {
				out.Summary.Changed++
			}
		}
		out.Groups, out.Summary = g.forest(parentVersion, kind, score, out.Summary)
		if !apply {
			return nil
		}
		for id, it := range g.items {
			s := it.s
			if s.ParentVersionID == parentVersion[id] && s.LinkKind == kind[id] {
				continue
			}
			s.ParentVersionID, s.LinkKind, s.LinkScore = parentVersion[id], kind[id], score[id]
			if err := q.UpdateSetup(&s); err != nil {
				return err
			}
		}
		return q.InsertEvent(store.Event{Game: gameID, Kind: "rebuilt", Details: map[string]any{
			"families": out.Summary.Families, "linked": out.Summary.Linked, "changed": out.Summary.Changed,
			"threshold": cfg.Threshold, "nameThreshold": cfg.NameThreshold}})
	})
	return out, err
}

// versionAt returns the newest version that existed at time t (nil if none).
func versionAt(vs []store.Version, t time.Time) *store.Version {
	var best *store.Version
	for i := range vs {
		if !vs[i].FileModified.After(t) {
			best = &vs[i]
		}
	}
	return best
}

// forest builds the trees of every car/track and counts them.
func (g *gameData) forest(parentVersion map[int64]int64, kind map[int64]string, score map[int64]float64, sum PreviewSummary) ([]GroupTrees, PreviewSummary) {
	var groups []GroupTrees
	keys := make([]string, 0, len(g.groups))
	for k := range g.groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		list := g.groups[k]
		nodes := map[int64]*Node{}
		for _, it := range list {
			nodes[it.s.ID] = g.node(it, parentVersion, kind, score)
		}
		gt := GroupTrees{Car: list[0].s.Car, Track: list[0].s.Track, Roots: []*Node{}}
		for _, it := range list {
			n := nodes[it.s.ID]
			if p := nodes[g.parentSetup(parentVersion, it.s.ID)]; p != nil && p != n {
				p.Children = append(p.Children, n)
				sum.Linked++
			} else {
				gt.Roots = append(gt.Roots, n)
				sum.Families++
			}
		}
		for _, r := range gt.Roots {
			sum.Longest = max(sum.Longest, depth(r))
		}
		groups = append(groups, gt)
	}
	return groups, sum
}

func (g *gameData) node(it *item, parentVersion map[int64]int64, kind map[int64]string, score map[int64]float64) *Node {
	n := &Node{SetupID: it.s.ID, Name: it.s.Name, Date: it.first, Session: it.s.Session, LinkKind: kind[it.s.ID],
		Score: score[it.s.ID], InFolder: it.s.RelPath != "", Versions: len(it.versions), Children: []*Node{}}
	if pv := parentVersion[it.s.ID]; pv != 0 {
		if p := g.items[g.versionOwn[pv]]; p != nil {
			for _, v := range p.versions {
				if v.ID == pv {
					n.ParentSeq = v.Seq
				}
			}
		}
	}
	return n
}

func depth(n *Node) int {
	d := 0
	for _, c := range n.Children {
		d = max(d, depth(c)+1)
	}
	return d
}

// ---------- setup page ----------

// VersionInfo is one version on the setup page.
type VersionInfo struct {
	store.Version
	Changes      string  `json:"changes"`      // vs the previous version, e.g. "TC -1, Rear wing +1"
	ChangedCount int     `json:"changedCount"` // number of scored settings that changed
	Score        float64 `json:"score"`
}

// ParentInfo describes where a setup came from.
type ParentInfo struct {
	SetupID   int64   `json:"setupId"`
	Name      string  `json:"name"`
	VersionID int64   `json:"versionId"`
	Seq       int     `json:"seq"`
	LinkKind  string  `json:"linkKind"`
	Score     float64 `json:"score"`
}

// RelinkOption is a version the user can pick as parent.
type RelinkOption struct {
	SetupID  int64              `json:"setupId"`
	Name     string             `json:"name"`
	Versions []RelinkOptVersion `json:"versions"`
}

// RelinkOptVersion is one version in a RelinkOption.
type RelinkOptVersion struct {
	ID   int64     `json:"id"`
	Seq  int       `json:"seq"`
	Date time.Time `json:"date"`
}

// Detail is everything the setup page shows.
type Detail struct {
	Setup    store.Setup    `json:"setup"`
	Versions []VersionInfo  `json:"versions"` // newest first
	Parent   *ParentInfo    `json:"parent"`
	Family   *Node          `json:"family"` // root of the family tree
	Options  []RelinkOption `json:"relinkOptions"`
}

// ErrNotFound is returned for unknown setups or versions.
var ErrNotFound = errors.New("not found")

// SetupDetail builds the setup page.
func SetupDetail(st *store.Store, id int64, parse Parser) (Detail, error) {
	var d Detail
	err := st.Tx(func(q store.Q) error {
		s, err := q.Setup(id)
		if err != nil {
			return err
		}
		if s == nil {
			return ErrNotFound
		}
		d.Setup = *s
		g, err := loadGame(q, s.Game)
		if err != nil {
			return err
		}
		cache := &fieldCache{q: q, parse: parse, byID: map[int64][]setup.Field{}}

		vs := g.items[id].versions
		d.Versions = make([]VersionInfo, 0, len(vs))
		for i := len(vs) - 1; i >= 0; i-- {
			vi := VersionInfo{Version: vs[i], Score: 100}
			if i > 0 {
				r := compare.Compare(cache.fields(&vs[i-1]), cache.fields(&vs[i]))
				vi.Changes, vi.ChangedCount, vi.Score = Summary(r, 6), r.Total-r.Same, r.Score
			}
			d.Versions = append(d.Versions, vi)
		}

		parentVersion := map[int64]int64{}
		kind := map[int64]string{}
		score := map[int64]float64{}
		for sid, it := range g.items {
			parentVersion[sid], kind[sid], score[sid] = it.s.ParentVersionID, it.s.LinkKind, it.s.LinkScore
		}
		if s.ParentVersionID != 0 {
			if p := g.items[g.versionOwn[s.ParentVersionID]]; p != nil {
				pi := &ParentInfo{SetupID: p.s.ID, Name: p.s.Name, VersionID: s.ParentVersionID, LinkKind: s.LinkKind, Score: s.LinkScore}
				for _, v := range p.versions {
					if v.ID == s.ParentVersionID {
						pi.Seq = v.Seq
					}
				}
				d.Parent = pi
			}
		}

		// Family tree: walk up to the root, then build downwards within the car/track.
		root := id
		for steps := 0; steps < 10000; steps++ {
			p := g.parentSetup(parentVersion, root)
			if p == 0 || p == id {
				break
			}
			root = p
		}
		list := g.groups[groupKey(s.Car, s.Track)]
		nodes := map[int64]*Node{}
		for _, it := range list {
			nodes[it.s.ID] = g.node(it, parentVersion, kind, score)
		}
		for _, it := range list {
			if p := nodes[g.parentSetup(parentVersion, it.s.ID)]; p != nil && it.s.ID != root {
				p.Children = append(p.Children, nodes[it.s.ID])
			}
		}
		d.Family = nodes[root]

		// Relink options: every other setup of the car/track that is not a descendant.
		for _, it := range list {
			if it.s.ID == id || g.isAncestor(parentVersion, id, it.s.ID) {
				continue
			}
			o := RelinkOption{SetupID: it.s.ID, Name: it.s.Name}
			for _, v := range it.versions {
				o.Versions = append(o.Versions, RelinkOptVersion{ID: v.ID, Seq: v.Seq, Date: v.FileModified})
			}
			d.Options = append(d.Options, o)
		}
		sort.Slice(d.Options, func(i, j int) bool { return strings.ToLower(d.Options[i].Name) < strings.ToLower(d.Options[j].Name) })
		return nil
	})
	return d, err
}

// Relink sets a setup's parent by hand (0 = no parent). Manual links are never changed
// automatically (D-62).
func Relink(st *store.Store, id, parentVersionID int64, parse Parser) error {
	return st.Tx(func(q store.Q) error {
		s, err := q.Setup(id)
		if err != nil {
			return err
		}
		if s == nil {
			return ErrNotFound
		}
		details := map[string]any{"name": s.Name}
		s.ParentVersionID, s.LinkKind, s.LinkScore = 0, store.LinkManual, 0
		if parentVersionID != 0 {
			pv, err := q.Version(parentVersionID)
			if err != nil {
				return err
			}
			if pv == nil {
				return ErrNotFound
			}
			p, err := q.Setup(pv.SetupID)
			if err != nil {
				return err
			}
			if p.Game != s.Game || !strings.EqualFold(p.Car, s.Car) || !strings.EqualFold(p.Track, s.Track) {
				return errors.New("a setup can only come from a setup of the same car and track")
			}
			g, err := loadGame(q, s.Game)
			if err != nil {
				return err
			}
			parentVersion := map[int64]int64{}
			for sid, it := range g.items {
				parentVersion[sid] = it.s.ParentVersionID
			}
			if g.isAncestor(parentVersion, s.ID, p.ID) {
				return errors.New("that setup was made from this one; select another")
			}
			own, err := q.LatestVersion(s.ID)
			if err != nil {
				return err
			}
			if own != nil {
				a, _ := parse(pv.Content)
				b, _ := parse(own.Content)
				s.LinkScore = compare.Compare(a, b).Score
			}
			s.ParentVersionID = parentVersionID
			details["parent"] = p.Name
		}
		if err := q.UpdateSetup(s); err != nil {
			return err
		}
		return q.InsertEvent(store.Event{Game: s.Game, Kind: "relinked", SetupID: s.ID, Details: details})
	})
}

// Info is what the user can edit on a setup page (nil = leave unchanged).
type Info struct {
	Note    *string      `json:"note"`
	Session *string      `json:"session"`
	Meta    *naming.Meta `json:"meta"`
}

// UpdateInfo saves a note, a session override or edited name fields. Session "auto" removes
// the override and lets SetupMan guess again (using enduranceMinutes, D-69).
func UpdateInfo(st *store.Store, id int64, in Info, enduranceMinutes int) (*store.Setup, error) {
	var out *store.Setup
	err := st.Tx(func(q store.Q) error {
		s, err := q.Setup(id)
		if err != nil {
			return err
		}
		if s == nil {
			return ErrNotFound
		}
		if in.Note != nil {
			s.Note = *in.Note
		}
		if in.Meta != nil {
			s.Meta, s.MetaManual = *in.Meta, true
			if s.Meta.Codes == nil {
				s.Meta.Codes = []string{}
			}
		}
		if in.Session != nil {
			switch *in.Session {
			case "auto":
				s.SessionManual = false
			case "", naming.SessionQ, naming.SessionR, naming.SessionEndu:
				s.Session, s.SessionManual = *in.Session, true
			default:
				return errors.New("unknown session")
			}
		}
		if !s.SessionManual {
			s.Session = naming.GuessSession(s.Meta, enduranceMinutes)
		}
		out = s
		return q.UpdateSetup(s)
	})
	return out, err
}

// VersionFields returns the fields of one stored version (for comparing versions).
func VersionFields(st *store.Store, versionID int64, parse Parser) (*store.Version, []setup.Field, error) {
	v, err := st.Version(versionID)
	if err != nil {
		return nil, nil, err
	}
	if v == nil {
		return nil, nil, ErrNotFound
	}
	f, err := parse(v.Content)
	return v, f, err
}

// TrackFamilies returns every family tree of one car on one track, with the current links:
// the biggest families first, then the newest. Used by the track page's Families tab.
func TrackFamilies(st *store.Store, gameID, car, track string) ([]*Node, error) {
	var roots []*Node
	err := st.Tx(func(q store.Q) error {
		g, err := loadGame(q, gameID)
		if err != nil {
			return err
		}
		k := groupKey(car, track)
		one := &gameData{items: g.items, versionOwn: g.versionOwn, groups: map[string][]*item{k: g.groups[k]}}
		if len(g.groups[k]) == 0 {
			roots = []*Node{}
			return nil
		}
		parentVersion := map[int64]int64{}
		kind := map[int64]string{}
		score := map[int64]float64{}
		for id, it := range g.items {
			parentVersion[id], kind[id], score[id] = it.s.ParentVersionID, it.s.LinkKind, it.s.LinkScore
		}
		groups, _ := one.forest(parentVersion, kind, score, PreviewSummary{})
		roots = groups[0].Roots
		return nil
	})
	sort.SliceStable(roots, func(i, j int) bool {
		si, sj := size(roots[i]), size(roots[j])
		if si != sj {
			return si > sj
		}
		return roots[i].Date.After(roots[j].Date)
	})
	return roots, err
}

func size(n *Node) int {
	s := 1
	for _, c := range n.Children {
		s += size(c)
	}
	return s
}
