// Package domain holds the field-trial data model, design checks and
// re-playable spatial-correction candidates for the field workbench.
package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Plot is one recorded field unit. A duplicated PlotID or (Row,Col)
// coordinates surface as blocking design issues rather than silent
// overwrites.
type Plot struct {
	ID        string  `json:"id"`
	Block     string  `json:"block"`
	Row       int     `json:"row"`
	Col       int     `json:"col"`
	Line      string  `json:"line"`
	Treatment string  `json:"treatment"`
	Yield     float64 `json:"yield"`
	YieldNA   bool    `json:"yield_na"`
	Height    float64 `json:"height"`
	HeightNA  bool    `json:"height_na"`
	Excluded  bool    `json:"excluded"`
	Edge      bool    `json:"edge"`
}

// Trait is a selectable measured attribute.
type Trait struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Traits lists the traits carried by the fixed fixture.
var Traits = []Trait{
	{Key: "yield", Name: "产量"},
	{Key: "height", Name: "株高"},
}

// Value returns the trait value and missing flag for a plot.
func (p Plot) Value(trait string) (float64, bool) {
	switch trait {
	case "height":
		return p.Height, p.HeightNA
	default:
		return p.Yield, p.YieldNA
	}
}

// DuplicateConflict describes one cell that is claimed by more than one
// record, or one experimental unit carrying two lines/treatments.
type DuplicateConflict struct {
	Kind  string   `json:"kind"`
	Key   string   `json:"key"`
	Plots []string `json:"plots"`
}

// SingleBlockTreatment flags a treatment that is observed in exactly one
// block, which makes any of its contrasts non-estimable.
type SingleBlockTreatment struct {
	Treatment string   `json:"treatment"`
	Block     string   `json:"block"`
	Plots     []string `json:"plots"`
}

// DesignReport is the pre-analysis audit of the trial layout.
type DesignReport struct {
	OK                bool                   `json:"ok"`
	RowCount          int                    `json:"row_count"`
	ColCount          int                    `json:"col_count"`
	PlotCount         int                    `json:"plot_count"`
	UsableCount       int                    `json:"usable_count"`
	MissingCount      map[string]int         `json:"missing_count"`
	ExcludedCount     int                    `json:"excluded_count"`
	EdgeCount         int                    `json:"edge_count"`
	Conflicts         []DuplicateConflict    `json:"conflicts"`
	SingleBlock       []SingleBlockTreatment `json:"single_block_treatments"`
	EstimablePairs    [][2]string            `json:"estimable_pairs"`
	NonEstimablePairs [][2]string            `json:"non_estimable_pairs"`
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AuditDesign performs the blocking checks against the current plot set.
// Border plots are flagged as edge using the rectangular bounding box of
// all recorded coordinates; missing traits never block design checks.
func AuditDesign(plots []Plot) DesignReport {
	r := DesignReport{MissingCount: map[string]int{}}
	if len(plots) == 0 {
		return r
	}
	minR, maxR, minC, maxC := plots[0].Row, plots[0].Row, plots[0].Col, plots[0].Col
	for _, p := range plots {
		if p.Row < minR {
			minR = p.Row
		}
		if p.Row > maxR {
			maxR = p.Row
		}
		if p.Col < minC {
			minC = p.Col
		}
		if p.Col > maxC {
			maxC = p.Col
		}
	}
	r.RowCount = maxR - minR + 1
	r.ColCount = maxC - minC + 1

	byID := map[string][]string{}
	byCell := map[string][]string{}
	byUnitLines := map[string][]string{}
	for i := range plots {
		p := &plots[i]
		byID[p.ID] = append(byID[p.ID], p.ID)
		cell := fmt.Sprintf("%d,%d", p.Row, p.Col)
		byCell[cell] = append(byCell[cell], p.ID)
		unit := p.ID
		byUnitLines[unit] = append(byUnitLines[unit], p.Line+"|"+p.Treatment)
	}
	for _, id := range sortedKeys(byID) {
		list := byID[id]
		if len(list) > 1 {
			r.Conflicts = append(r.Conflicts, DuplicateConflict{
				Kind: "duplicate_plot_id", Key: id, Plots: list,
			})
		}
	}
	for _, cell := range sortedKeys(byCell) {
		list := byCell[cell]
		if len(list) > 1 {
			r.Conflicts = append(r.Conflicts, DuplicateConflict{
				Kind: "duplicate_coordinate", Key: cell, Plots: append([]string(nil), list...),
			})
		}
	}
	// Same plot id carrying two different lines or treatments is rejected.
	seen := map[string]map[string]bool{}
	idPlots := map[string][]string{}
	for _, p := range plots {
		if seen[p.ID] == nil {
			seen[p.ID] = map[string]bool{}
		}
		seen[p.ID][p.Line+"|"+p.Treatment] = true
		idPlots[p.ID] = append(idPlots[p.ID], p.ID)
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if len(seen[id]) > 1 {
			r.Conflicts = append(r.Conflicts, DuplicateConflict{
				Kind: "unit_two_lines", Key: id, Plots: idPlots[id],
			})
		}
	}

	blocksByTreat := map[string]map[string]bool{}
	treatPlots := map[string][]string{}
	var treatments []string
	tSeen := map[string]bool{}
	usable := 0
	for i := range plots {
		p := &plots[i]
		p.Edge = p.Row == minR || p.Row == maxR || p.Col == minC || p.Col == maxC
		if p.Edge {
			r.EdgeCount++
		}
		if p.Excluded {
			r.ExcludedCount++
		}
		if p.YieldNA {
			r.MissingCount["yield"]++
		}
		if p.HeightNA {
			r.MissingCount["height"]++
		}
		if !p.Excluded {
			usable++
		}
		if blocksByTreat[p.Treatment] == nil {
			blocksByTreat[p.Treatment] = map[string]bool{}
		}
		blocksByTreat[p.Treatment][p.Block] = true
		treatPlots[p.Treatment] = append(treatPlots[p.Treatment], p.ID)
		if !tSeen[p.Treatment] {
			tSeen[p.Treatment] = true
			treatments = append(treatments, p.Treatment)
		}
	}
	sort.Strings(treatments)
	r.PlotCount = len(plots)
	r.UsableCount = usable
	for _, t := range treatments {
		blocks := blocksByTreat[t]
		if len(blocks) == 1 {
			var b string
			for k := range blocks {
				b = k
			}
			r.SingleBlock = append(r.SingleBlock, SingleBlockTreatment{
				Treatment: t, Block: b, Plots: treatPlots[t],
			})
		}
	}

	for i := 0; i < len(treatments); i++ {
		for j := i + 1; j < len(treatments); j++ {
			a, b := treatments[i], treatments[j]
			pair := [2]string{a, b}
			if len(blocksByTreat[a]) >= 2 && len(blocksByTreat[b]) >= 2 {
				r.EstimablePairs = append(r.EstimablePairs, pair)
			} else {
				r.NonEstimablePairs = append(r.NonEstimablePairs, pair)
			}
		}
	}
	r.OK = len(r.Conflicts) == 0
	return r
}

// PairLabel formats a treatment pair.
func PairLabel(p [2]string) string { return p[0] + " - " + p[1] }

// ParsePair parses "T1-T2" style pair keys back into treatments.
func ParsePair(s string) ([2]string, bool) {
	parts := strings.SplitN(s, "--", 2)
	if len(parts) != 2 {
		return [2]string{}, false
	}
	return [2]string{parts[0], parts[1]}, true
}
