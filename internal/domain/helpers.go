package domain

import (
	"math"
	"sort"
)

// entryState carries the fitting state for one plot.
type entryState struct {
	p      Plot
	used   bool
	x      float64 // local-neighbour covariate (mean orthogonal residual)
	xn     int     // number of in-field neighbours contributing
	fitted float64
	resid  float64
	adjust float64
}

// neighbourGrid indexes used observations by field coordinate. Missing
// plots, excluded plots and coordinates outside the bounding box are simply
// absent, so neighbours are never wrapped around a field edge.
type neighbourGrid struct {
	cells map[[2]int][]int
	used  []bool
}

func newNeighbourGrid(entries []entryState) neighbourGrid {
	g := neighbourGrid{cells: map[[2]int][]int{}, used: make([]bool, len(entries))}
	for i := range entries {
		if entries[i].used {
			key := [2]int{entries[i].p.Row, entries[i].p.Col}
			g.cells[key] = append(g.cells[key], i)
			g.used[i] = true
		}
	}
	return g
}

// covariate returns the mean residual of the four orthogonal neighbours.
// No torus/wrap lookup is performed: border plots only see real neighbours.
func (g neighbourGrid) covariate(entries []entryState, row, col int) (float64, int) {
	sum, n := 0.0, 0
	deltas := [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	for _, d := range deltas {
		for _, idx := range g.cells[[2]int{row + d[0], col + d[1]}] {
			if g.used[idx] {
				sum += entries[idx].resid
				n++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

func centerZero(v []float64) {
	if len(v) == 0 {
		return
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	s /= float64(len(v))
	for i := range v {
		v[i] -= s
	}
}

type meanSet struct {
	mean float64
	ids  []string
}

func rawMeansByTreatment(plots []Plot, os []obs, trait string) map[string]float64 {
	sums, counts := map[string]float64{}, map[string]int{}
	for _, o := range os {
		p := plots[o.idx]
		y, na := p.Value(trait)
		if na {
			continue
		}
		sums[p.Treatment] += y
		counts[p.Treatment]++
	}
	out := map[string]float64{}
	for t, s := range sums {
		out[t] = s / float64(counts[t])
	}
	return out
}

func adjustedMeans(entries []entryState, os []obs, treatments []string, plots []Plot) map[string]meanSet {
	sums := make([]float64, len(treatments))
	counts := make([]int, len(treatments))
	ids := make([][]string, len(treatments))
	tidx := map[string]int{}
	for i, t := range treatments {
		tidx[t] = i
	}
	for _, o := range os {
		t := tidx[plots[o.idx].Treatment]
		sums[t] += entries[o.idx].adjust
		counts[t]++
		ids[t] = append(ids[t], plots[o.idx].ID)
	}
	out := map[string]meanSet{}
	for i, t := range treatments {
		if counts[i] > 0 {
			out[t] = meanSet{mean: sums[i] / float64(counts[i]), ids: ids[i]}
		}
	}
	return out
}

func blocksByTreatment(os []obs, plots []Plot, treatments []string) map[string][]string {
	sets := map[string]map[string]bool{}
	for _, o := range os {
		t := plots[o.idx].Treatment
		if sets[t] == nil {
			sets[t] = map[string]bool{}
		}
		sets[t][plots[o.idx].Block] = true
	}
	out := map[string][]string{}
	for t, s := range sets {
		for b := range s {
			out[t] = append(out[t], b)
		}
		sort.Strings(out[t])
	}
	return out
}

func contrastSE(entries []entryState, os []obs, model, a, b string, treatments []string) float64 {
	rss := 0.0
	for _, o := range os {
		rss += entries[o.idx].resid * entries[o.idx].resid
	}
	df := len(os) - len(treatments)
	switch model {
	case ModelRowCol:
		df -= countLevels(os, true) + countLevels(os, false) - 1
	case ModelBlock, ModelLocal:
		df -= blockLevels(os)
		if model == ModelLocal {
			df--
		}
	}
	if df < 1 {
		return 0
	}
	s2 := rss / float64(df)
	na, nb := 0, 0
	for _, o := range os {
		switch entries[o.idx].p.Treatment {
		case a:
			na++
		case b:
			nb++
		}
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return math.Sqrt(s2 * (1/float64(na) + 1/float64(nb)))
}

func countLevels(os []obs, rows bool) int {
	set := map[int]bool{}
	for _, o := range os {
		if rows {
			set[o.row] = true
		} else {
			set[o.col] = true
		}
	}
	return len(set)
}

func blockLevels(os []obs) int {
	set := map[int]bool{}
	for _, o := range os {
		set[o.block] = true
	}
	return len(set)
}
