package compare

import (
	"reflect"
	"testing"
)

func pair(a, b int, score float64) Pair {
	return Pair{A: a, B: b, Result: Result{Score: score, Identical: score == 100}}
}

func TestCluster(t *testing.T) {
	// 0 and 1 identical; 2 near 1 (96); 3 near 4 (97); 5 alone.
	fps := []string{"x", "x", "y", "z", "w", "v"}
	names := []string{"a", "b", "c", "d", "e", "f"}
	var pairs []Pair
	scores := map[[2]int]float64{{0, 1}: 100, {0, 2}: 96, {1, 2}: 96, {3, 4}: 97}
	for i := 0; i < 6; i++ {
		for j := i + 1; j < 6; j++ {
			s := scores[[2]int{i, j}]
			if s == 0 {
				s = 50
			}
			pairs = append(pairs, pair(i, j, s))
		}
	}

	c := Cluster(fps, names, pairs, 95)
	if !reflect.DeepEqual(c.Identical, [][]int{{0, 1}}) {
		t.Errorf("identical = %v", c.Identical)
	}
	if len(c.Near) != 2 || !reflect.DeepEqual(c.Near[0].Members, []int{0, 1, 2}) || !reflect.DeepEqual(c.Near[1].Members, []int{3, 4}) {
		t.Errorf("near = %+v", c.Near)
	}
	if len(c.Near[0].Pairs) != 2 { // 0-2 and 1-2 (the identical 0-1 pair is not listed)
		t.Errorf("near pairs = %v", c.Near[0].Pairs)
	}
	if !reflect.DeepEqual(c.Unique, []int{5}) {
		t.Errorf("unique = %v", c.Unique)
	}
	if len(c.Order) != 6 || c.Order[5] != 5 {
		t.Errorf("order = %v", c.Order)
	}
	if c.Cluster[0] != 1 || c.Cluster[3] != 2 || c.Cluster[5] != 0 {
		t.Errorf("cluster ids = %v", c.Cluster)
	}
	if !c.HasDuplicates() {
		t.Error("HasDuplicates should be true")
	}

	// Raising the threshold breaks the near clusters but keeps the identical one.
	c = Cluster(fps, names, pairs, 99)
	if len(c.Near) != 0 || len(c.Identical) != 1 || len(c.Unique) != 4 {
		t.Errorf("at 99%%: %+v", c)
	}
}
