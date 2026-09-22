package domain

import (
	"math"
	"sort"
)

// Model identifiers and Chinese labels for branch candidates.
const (
	ModelRowCol = "rowcol"
	ModelBlock  = "block"
	ModelLocal  = "local"
)

// ModelName returns the UI label for a model key.
func ModelName(m string) string {
	switch m {
	case ModelBlock:
		return "区组随机效应"
	case ModelLocal:
		return "局部相关"
	default:
		return "行列趋势"
	}
}

// PlotResult is the per-plot decomposition for a fitted branch.
type PlotResult struct {
	PlotID    string  `json:"plot_id"`
	Block     string  `json:"block"`
	Row       int     `json:"row"`
	Col       int     `json:"col"`
	Line      string  `json:"line"`
	Treatment string  `json:"treatment"`
	Raw       float64 `json:"raw"`
	RawNA     bool    `json:"raw_na"`
	Excluded  bool    `json:"excluded"`
	Edge      bool    `json:"edge"`
	Fitted    float64 `json:"fitted"`
	Residual  float64 `json:"residual"`
	Adjusted  float64 `json:"adjusted"`
}

// ContrastResult binds a treatment comparison to its coverage.
type ContrastResult struct {
	Pair            [2]string `json:"pair"`
	Estimable       bool      `json:"estimable"`
	Reason          string    `json:"reason"`
	BlocksA         []string  `json:"blocks_a"`
	BlocksB         []string  `json:"blocks_b"`
	NAdjustedA      int       `json:"n_adjusted_a"`
	NAdjustedB      int       `json:"n_adjusted_b"`
	RawDiff         float64   `json:"raw_diff"`
	AdjustedDiff    float64   `json:"adjusted_diff"`
	CorrectionDelta float64   `json:"correction_delta"`
	StandardError   float64   `json:"standard_error"`
}

// FitResult is a deterministic, replayable branch analysis result.
type FitResult struct {
	Model       string           `json:"model"`
	Trait       string           `json:"trait"`
	ExcludeEdge bool             `json:"exclude_edge"`
	Iterations  int              `json:"iterations"`
	Converged   bool             `json:"converged"`
	Plots       []PlotResult     `json:"plots"`
	Contrasts   []ContrastResult `json:"contrasts"`
}

const (
	lambdaBlock = 4.0
	lambdaLocal = 3.0
	maxIters    = 200
	tol         = 1e-8
)

type obs struct {
	idx       int
	y         float64
	row, col  int
	block     int
	treatment int
}

func round6(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1e6) / 1e6
}

// Fit fits the requested candidate model. No RNG is used: the coordinate
// sweep is deterministic for a given snapshot, so the same branch replays
// identically. Missing observations are dropped (never treated as zero) and
// edge exclusion is applied at the branch/snapshot level.
func Fit(model, trait string, excludeEdge bool, plots []Plot) FitResult {
	entries := make([]entryState, len(plots))
	for i, p := range plots {
		entries[i].p = p
	}
	rowIndex := map[int]int{}
	colIndex := map[int]int{}
	blockIndex := map[string]int{}
	treatIndex := map[string]int{}
	var os []obs
	for i := range entries {
		p := &entries[i].p
		_, na := p.Value(trait)
		if na || p.Excluded || (excludeEdge && p.Edge) {
			continue
		}
		y, _ := p.Value(trait)
		if _, ok := rowIndex[p.Row]; !ok {
			rowIndex[p.Row] = len(rowIndex)
		}
		if _, ok := colIndex[p.Col]; !ok {
			colIndex[p.Col] = len(colIndex)
		}
		if _, ok := blockIndex[p.Block]; !ok {
			blockIndex[p.Block] = len(blockIndex)
		}
		if _, ok := treatIndex[p.Treatment]; !ok {
			treatIndex[p.Treatment] = len(treatIndex)
		}
		os = append(os, obs{
			idx: i, y: y, row: rowIndex[p.Row], col: colIndex[p.Col],
			block: blockIndex[p.Block], treatment: treatIndex[p.Treatment],
		})
		entries[i].used = true
	}

	treatments := make([]string, len(treatIndex))
	for k, v := range treatIndex {
		treatments[v] = k
	}
	sort.Strings(treatments)
	for k, v := range treatments {
		treatIndex[v] = k
	}
	for k := range os {
		os[k].treatment = treatIndex[plots[os[k].idx].Treatment]
	}

	nr, nc, nb, nt := len(rowIndex), len(colIndex), len(blockIndex), len(treatments)
	mu := 0.0
	alpha := make([]float64, nr)
	gamma := make([]float64, nc)
	beta := make([]float64, nb)
	tau := make([]float64, nt)
	theta := 0.0
	if len(os) > 0 {
		for _, o := range os {
			mu += o.y
		}
		mu /= float64(len(os))
	}

	grid := newNeighbourGrid(entries)
	sys := func(o *obs) float64 {
		s := mu + tau[o.treatment]
		switch model {
		case ModelBlock:
			s += beta[o.block]
		case ModelLocal:
			s += beta[o.block] + theta*entries[o.idx].x
		default:
			s += alpha[o.row] + gamma[o.col]
		}
		return s
	}
	refreshResiduals := func() {
		for k := range os {
			o := &os[k]
			entries[o.idx].resid = o.y - sys(o)
		}
	}

	iters, converged := 0, false
	for ; iters < maxIters; iters++ {
		maxDelta := 0.0
		// local covariate uses residuals from the previous sweep
		if model == ModelLocal {
			refreshResiduals()
			for k := range os {
				o := &os[k]
				x, n := grid.covariate(entries, plots[o.idx].Row, plots[o.idx].Col)
				entries[o.idx].x = x
				entries[o.idx].xn = n
			}
		}

		// fixed effects: treatment
		counts := make([]int, nt)
		sums := make([]float64, nt)
		for k := range os {
			o := &os[k]
			counts[o.treatment]++
			sums[o.treatment] += o.y - sys(o) + tau[o.treatment]
		}
		for t := 0; t < nt; t++ {
			if counts[t] > 0 {
				nv := sums[t]/float64(counts[t]) - mu
				maxDelta = math.Max(maxDelta, math.Abs(nv-tau[t]))
				tau[t] = nv
			}
		}
		centerZero(tau)
		// absorb grand mean shift after centering treatments
		nm := 0.0
		for k := range os {
			nm += os[k].y - sys(&os[k])
		}
		nm /= float64(len(os))
		maxDelta = math.Max(maxDelta, math.Abs(nm))
		mu += nm

		switch model {
		case ModelBlock, ModelLocal:
			bc := make([]int, nb)
			bs := make([]float64, nb)
			for k := range os {
				o := &os[k]
				bc[o.block]++
				bs[o.block] += o.y - sys(o) + beta[o.block]
			}
			for b := 0; b < nb; b++ {
				nv := bs[b] / (float64(bc[b]) + lambdaBlock)
				maxDelta = math.Max(maxDelta, math.Abs(nv-beta[b]))
				beta[b] = nv
			}
			if model == ModelLocal {
				num, den := 0.0, 0.0
				for k := range os {
					o := &os[k]
					x := entries[o.idx].x
					res := o.y - sys(o) + theta*x
					num += x * res
					den += x*x + lambdaLocal
				}
				if den > 0 {
					nv := num / den
					maxDelta = math.Max(maxDelta, math.Abs(nv-theta))
					theta = nv
				}
			}
		default:
			rc := make([]int, nr)
			rs := make([]float64, nr)
			cc := make([]int, nc)
			cs := make([]float64, nc)
			for k := range os {
				o := &os[k]
				rc[o.row]++
				rs[o.row] += o.y - sys(o) + alpha[o.row]
				cc[o.col]++
				cs[o.col] += o.y - sys(o) + gamma[o.col]
			}
			for r := 0; r < nr; r++ {
				if rc[r] > 0 {
					nv := rs[r] / float64(rc[r])
					maxDelta = math.Max(maxDelta, math.Abs(nv-alpha[r]))
					alpha[r] = nv
				}
			}
			for c := 0; c < nc; c++ {
				if cc[c] > 0 {
					nv := cs[c] / float64(cc[c])
					maxDelta = math.Max(maxDelta, math.Abs(nv-gamma[c]))
					gamma[c] = nv
				}
			}
			centerZero(alpha)
			centerZero(gamma)
		}

		if maxDelta < tol {
			converged = true
			iters++
			break
		}
	}
	refreshResiduals()

	for k := range os {
		o := &os[k]
		f := sys(o)
		entries[o.idx].fitted = f
		spatial := 0.0
		switch model {
		case ModelBlock:
			spatial = beta[o.block]
		case ModelLocal:
			spatial = beta[o.block] + theta*entries[o.idx].x
		default:
			spatial = alpha[o.row] + gamma[o.col]
		}
		entries[o.idx].adjust = o.y - spatial
	}

	return assemble(model, trait, excludeEdge, iters, converged, entries, os, plots, treatments)
}
