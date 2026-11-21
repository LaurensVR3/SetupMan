package compare

import (
	"sort"
	"strings"
)

// Clusters describes how the setups of one car+track group together at a threshold.
// All numbers are indexes into the list of setups (or into the list of pairs).
type Clusters struct {
	Identical [][]int       `json:"identical"` // groups of setups with equal fingerprints
	Near      []NearCluster `json:"near"`      // connected groups at or above the threshold
	Unique    []int         `json:"unique"`    // setups that match nothing
	Order     []int         `json:"order"`     // display order: similar setups next to each other
	Cluster   []int         `json:"cluster"`   // per setup: cluster number (0 = unique), for colouring
}

// NearCluster is a group of near-identical setups.
type NearCluster struct {
	Members []int `json:"members"`
	Pairs   []int `json:"pairs"` // indexes into the pair list, best score first
}

// HasDuplicates reports whether there is at least one identical or near-identical cluster.
func (c Clusters) HasDuplicates() bool { return len(c.Identical)+len(c.Near) > 0 }

// Cluster groups setups. names is only used to sort deterministically.
// A pair belongs together when it is identical or its score is >= threshold.
func Cluster(fingerprints, names []string, pairs []Pair, threshold float64) Clusters {
	n := len(fingerprints)
	res := Clusters{Identical: [][]int{}, Near: []NearCluster{}, Unique: []int{}, Order: []int{}, Cluster: make([]int, n)}
	byName := func(list []int) {
		sort.Slice(list, func(i, j int) bool { return strings.ToLower(names[list[i]]) < strings.ToLower(names[list[j]]) })
	}

	// Identical: same fingerprint.
	byFp := map[string][]int{}
	for i, fp := range fingerprints {
		byFp[fp] = append(byFp[fp], i)
	}
	for _, m := range byFp {
		if len(m) > 1 {
			byName(m)
			res.Identical = append(res.Identical, m)
		}
	}
	sort.Slice(res.Identical, func(i, j int) bool { return res.Identical[i][0] < res.Identical[j][0] })

	// Connected groups (union-find) over pairs at or above the threshold.
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	var edges []int
	for i, p := range pairs {
		if p.Identical || p.Score >= threshold {
			edges = append(edges, i)
			parent[find(p.A)] = find(p.B)
		}
	}
	comps := map[int][]int{}
	for i := 0; i < n; i++ {
		comps[find(i)] = append(comps[find(i)], i)
	}

	var groups [][]int
	for _, members := range comps {
		if len(members) == 1 {
			res.Unique = append(res.Unique, members[0])
			continue
		}
		byName(members)
		groups = append(groups, members)
	}
	byName(res.Unique)
	// Biggest groups first, then alphabetical.
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i]) != len(groups[j]) {
			return len(groups[i]) > len(groups[j])
		}
		return strings.ToLower(names[groups[i][0]]) < strings.ToLower(names[groups[j][0]])
	})

	for gi, members := range groups {
		inside := map[int]bool{}
		for _, m := range members {
			inside[m] = true
			res.Cluster[m] = gi + 1
		}
		res.Order = append(res.Order, chain(members, pairs)...)

		fps := map[string]bool{}
		for _, m := range members {
			fps[fingerprints[m]] = true
		}
		if len(fps) == 1 {
			continue // all identical: listed under Identical only
		}
		nc := NearCluster{Members: members}
		for _, e := range edges {
			if inside[pairs[e].A] && !pairs[e].Identical {
				nc.Pairs = append(nc.Pairs, e)
			}
		}
		sort.SliceStable(nc.Pairs, func(i, j int) bool { return pairs[nc.Pairs[i]].Score > pairs[nc.Pairs[j]].Score })
		res.Near = append(res.Near, nc)
	}
	res.Order = append(res.Order, res.Unique...)
	return res
}

// chain orders members so each one is next to the one it is most similar to
// (start with the first, repeatedly append the most similar remaining member).
func chain(members []int, pairs []Pair) []int {
	score := make(map[[2]int]float64, len(pairs))
	for _, p := range pairs {
		score[[2]int{p.A, p.B}] = p.Score
		score[[2]int{p.B, p.A}] = p.Score
	}
	left := append([]int(nil), members[1:]...)
	out := []int{members[0]}
	for len(left) > 0 {
		last := out[len(out)-1]
		best := 0
		for i := range left {
			if score[[2]int{last, left[i]}] > score[[2]int{last, left[best]}] {
				best = i
			}
		}
		out = append(out, left[best])
		left = append(left[:best], left[best+1:]...)
	}
	return out
}
