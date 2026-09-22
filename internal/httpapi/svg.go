package httpapi

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"fieldbench/internal/domain"
	"fieldbench/internal/store"
)

const (
	cellSize = 64
	padLeft  = 70
	padTop   = 40
)

type layerSpec struct {
	key   string
	title string
}

// Layers are selectable grid overlays.
var Layers = []layerSpec{
	{"line", "品系分布"},
	{"raw", "原始性状"},
	{"residual", "残差"},
	{"adjusted", "空间校正值"},
}

func hsl(h float64) string {
	return fmt.Sprintf("hsl(%.0f,65%%,72%%)", h)
}

// RenderGrid writes an SVG of the current trial or a published branch.
func RenderGrid(w io.Writer, plots []domain.Plot, design domain.DesignReport,
	branch *store.Branch, trait, layer string) error {
	if layer == "" {
		layer = "line"
	}
	if branch != nil && trait == "" {
		trait = branch.Trait
	}
	if trait == "" {
		trait = "yield"
	}
	results := map[string]domain.PlotResult{}
	if branch != nil {
		for _, pr := range branch.Result.Plots {
			results[pr.PlotID] = pr
		}
	}

	minR, maxR, minC, maxC := bounds(plots)
	if len(plots) == 0 {
		minR, minC = 1, 1
	}
	rows := maxR - minR + 1
	cols := maxC - minC + 1
	width := padLeft + cols*cellSize + 30
	height := padTop + rows*cellSize + 150

	cellOwners := map[string][]string{}
	for _, p := range plots {
		k := fmt.Sprintf("%d:%d", p.Row, p.Col)
		cellOwners[k] = append(cellOwners[k], p.ID)
	}
	var vals []float64
	for _, p := range plots {
		v, ok := cellValue(p, results, trait, layer)
		if ok {
			vals = append(vals, v)
		}
	}
	lo, hi := 0.0, 1.0
	if len(vals) > 0 {
		lo, hi = vals[0], vals[0]
		for _, v := range vals[1:] {
			lo = math.Min(lo, v)
			hi = math.Max(hi, v)
		}
	}
	treatHue := treatmentColors(plots)

	bw := &strings.Builder{}
	fmt.Fprintf(bw, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="sans-serif">`,
		width, height, width, height)
	fmt.Fprintf(bw, `<rect width="100%%" height="100%%" fill="#fafafa"/>`)
	fmt.Fprintf(bw, `<text x="%d" y="24" font-size="18" font-weight="bold">田区空间校正台 · %s</text>`,
		padLeft, layerTitle(layer))
	if branch != nil {
		fmt.Fprintf(bw, `<text x="%d" y="24" font-size="12">分支 #%d %s / %s</text>`,
			width-260, branch.ID, domain.ModelName(branch.Model), traitName(trait))
	}

	for c := minC; c <= maxC; c++ {
		x := padLeft + (c-minC)*cellSize + cellSize/2
		fmt.Fprintf(bw, `<text x="%d" y="%d" text-anchor="middle" font-size="12" fill="#555">列%d</text>`,
			x, padTop-6, c)
	}
	for r := minR; r <= maxR; r++ {
		y := padTop + (r-minR)*cellSize + cellSize/2 + 4
		fmt.Fprintf(bw, `<text x="%d" y="%d" text-anchor="end" font-size="12" fill="#555">行%d</text>`,
			padLeft-8, y, r)
	}

	for _, p := range plots {
		x := padLeft + (p.Col-minC)*cellSize
		y := padTop + (p.Row-minR)*cellSize
		fill := "#ffffff"
		switch layer {
		case "line":
			fill = hsl(treatHue[p.Treatment])
		default:
			if v, ok := cellValue(p, results, trait, layer); ok {
				fill = valueColor(v, lo, hi)
			}
		}
		stroke := "#999"
		strokeW := 1
		if len(cellOwners[fmt.Sprintf("%d:%d", p.Row, p.Col)]) > 1 {
			stroke = "#d11"
			strokeW = 4
		}
		opacity := 1.0
		if p.Excluded {
			opacity = 0.35
		}
		fmt.Fprintf(bw, `<g opacity="%.2f"><rect x="%d" y="%d" width="%d" height="%d" fill="%s" stroke="%s" stroke-width="%d"/>`,
			opacity, x, y, cellSize, cellSize, fill, stroke, strokeW)
		_, na := p.Value(trait)
		if na && layer != "line" {
			fmt.Fprintf(bw, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#c9a227" stroke-width="3"/>`,
				x+6, y+6, x+cellSize-6, y+cellSize-6)
			fmt.Fprintf(bw, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#c9a227" stroke-width="3"/>`,
				x+cellSize-6, y+6, x+6, y+cellSize-6)
		}
		title1 := p.ID + " " + p.Line + " " + p.Treatment + " [" + p.Block + "]"
		if na {
			title1 += " 缺区"
		}
		if p.Edge {
			title1 += " 边缘"
		}
		fmt.Fprintf(bw, `<text x="%d" y="%d" font-size="11" font-weight="bold">%s</text>`,
			x+5, y+15, escapeXML(title1))
		if layer == "line" {
			fmt.Fprintf(bw, `<text x="%d" y="%d" font-size="11">%s</text>`,
				x+5, y+32, escapeXML(p.Line))
		} else {
			txt := "缺区"
			if v, ok := cellValue(p, results, trait, layer); ok {
				txt = fmt.Sprintf("%.2f", v)
			}
			fmt.Fprintf(bw, `<text x="%d" y="%d" font-size="13">%s</text>`,
				x+5, y+36, txt)
		}
		if p.Edge {
			fmt.Fprintf(bw, `<circle cx="%d" cy="%d" r="4" fill="#2266cc"/>`,
				x+cellSize-8, y+8)
		}
		fmt.Fprint(bw, `</g>`)
	}

	legendY := padTop + rows*cellSize + 28
	writeLegend(bw, plots, design, branch, layer, lo, hi, treatHue, legendY, width)
	fmt.Fprint(bw, `</svg>`)
	_, err := io.WriteString(w, bw.String())
	return err
}

func layerTitle(layer string) string {
	for _, l := range Layers {
		if l.key == layer {
			return l.title
		}
	}
	return "品系分布"
}

func traitName(t string) string {
	for _, tr := range domain.Traits {
		if tr.Key == t {
			return tr.Name
		}
	}
	return t
}

func bounds(plots []domain.Plot) (int, int, int, int) {
	if len(plots) == 0 {
		return 1, 1, 1, 1
	}
	minR, maxR := plots[0].Row, plots[0].Row
	minC, maxC := plots[0].Col, plots[0].Col
	for _, p := range plots {
		minR = min(minR, p.Row)
		maxR = max(maxR, p.Row)
		minC = min(minC, p.Col)
		maxC = max(maxC, p.Col)
	}
	return minR, maxR, minC, maxC
}

func treatmentColors(plots []domain.Plot) map[string]float64 {
	set := map[string]bool{}
	var ts []string
	for _, p := range plots {
		if !set[p.Treatment] {
			set[p.Treatment] = true
			ts = append(ts, p.Treatment)
		}
	}
	sort.Strings(ts)
	out := map[string]float64{}
	for i, t := range ts {
		out[t] = math.Mod(float64(i*360/len(ts)), 360)
	}
	return out
}

func cellValue(p domain.Plot, results map[string]domain.PlotResult, trait, layer string) (float64, bool) {
	_, na := p.Value(trait)
	switch layer {
	case "raw":
		if na {
			return 0, false
		}
		v, _ := p.Value(trait)
		return v, true
	case "residual", "adjusted":
		pr, ok := results[p.ID]
		if !ok || p.Excluded {
			return 0, false
		}
		if pr.RawNA {
			return 0, false
		}
		if layer == "residual" {
			return pr.Residual, true
		}
		return pr.Adjusted, true
	}
	return 0, false
}

func valueColor(v, lo, hi float64) string {
	if hi <= lo {
		return "hsl(120,50%,85%)"
	}
	t := (v - lo) / (hi - lo)
	hue := 120 - 120*t
	return fmt.Sprintf("hsl(%.0f,60%%,80%%)", hue)
}

func writeLegend(bw *strings.Builder, plots []domain.Plot, design domain.DesignReport,
	branch *store.Branch, layer string, lo, hi float64, hues map[string]float64, y, width int) {
	fmt.Fprintf(bw, `<text x="%d" y="%d" font-size="12" font-weight="bold">图例</text>`, padLeft, y)
	items := []string{
		`<circle cx="0" cy="0" r="4" fill="#2266cc"/> 边缘地块（不使用环绕邻居）`,
		`<rect x="-6" y="-6" width="12" height="12" fill="none" stroke="#d11" stroke-width="3"/> 重复坐标/冲突`,
		`<line x1="-6" y1="-6" x2="6" y2="6" stroke="#c9a227" stroke-width="3"/> 缺区（不作零）`,
	}
	x := padLeft
	for _, it := range items {
		fmt.Fprintf(bw, `<g transform="translate(%d,%d)">%s</g>`, x+12, y+22, it)
		x += 230
	}
	if layer == "line" {
		var ts []string
		for t := range hues {
			ts = append(ts, t)
		}
		sort.Strings(ts)
		x = padLeft
		for _, t := range ts {
			fmt.Fprintf(bw, `<g transform="translate(%d,%d)"><rect width="14" height="14" fill="%s"/><text x="18" y="12" font-size="12">%s</text></g>`,
				x, y+44, hsl(hues[t]), t)
			x += 70
		}
	} else {
		fmt.Fprintf(bw, `<text x="%d" y="%d" font-size="12">低 %.2f</text><rect x="%d" y="%d" width="160" height="14" fill="url(#grad)"/><text x="%d" y="%d" font-size="12">%.2f 高</text>`,
			padLeft, y+56, lo, padLeft+55, y+44, padLeft+222, y+56, hi)
		fmt.Fprintf(bw, `<defs><linearGradient id="grad"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></linearGradient></defs>`,
			valueColor(lo, lo, hi), valueColor(hi, lo, hi))
	}
	if !design.OK {
		fmt.Fprintf(bw, `<text x="%d" y="%d" font-size="13" fill="#d11" font-weight="bold">设计检查未通过：%d 个冲突，分析已阻止</text>`,
			padLeft, y+88, len(design.Conflicts))
	} else if branch != nil && branch.Status == "blocked" {
		fmt.Fprintf(bw, `<text x="%d" y="%d" font-size="13" fill="#d11">%s</text>`,
			padLeft, y+88, escapeXML(branch.Reason))
	}
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
