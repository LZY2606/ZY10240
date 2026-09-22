package domain

import "strconv"

func assemble(model, trait string, excludeEdge bool, iters int, converged bool,
	entries []entryState, os []obs, plots []Plot, treatments []string) FitResult {
	res := FitResult{
		Model: model, Trait: trait, ExcludeEdge: excludeEdge,
		Iterations: iters, Converged: converged,
	}
	for i := range entries {
		p := entries[i].p
		y, na := p.Value(trait)
		pr := PlotResult{
			PlotID: p.ID, Block: p.Block, Row: p.Row, Col: p.Col,
			Line: p.Line, Treatment: p.Treatment,
			Raw: round6(y), RawNA: na, Excluded: p.Excluded, Edge: p.Edge,
		}
		if entries[i].used {
			pr.Fitted = round6(entries[i].fitted)
			pr.Residual = round6(entries[i].resid)
			pr.Adjusted = round6(entries[i].adjust)
		}
		res.Plots = append(res.Plots, pr)
	}

	rawMean := rawMeansByTreatment(plots, os, trait)
	adjMean := adjustedMeans(entries, os, treatments, plots)
	blockSets := blocksByTreatment(os, plots, treatments)
	for i := 0; i < len(treatments); i++ {
		for j := i + 1; j < len(treatments); j++ {
			a, b := treatments[i], treatments[j]
			c := ContrastResult{Pair: [2]string{a, b}}
			c.BlocksA = blockSets[a]
			c.BlocksB = blockSets[b]
			am, aok := adjMean[a]
			bm, bok := adjMean[b]
			c.NAdjustedA = len(am.ids)
			c.NAdjustedB = len(bm.ids)
			estimable := len(c.BlocksA) >= 2 && len(c.BlocksB) >= 2 && aok && bok
			c.Estimable = estimable
			switch {
			case !estimable && len(c.BlocksA) < 2:
				c.Reason = "处理 " + a + " 仅出现在单一区组，不使用空间平滑伪造重复"
			case !estimable && len(c.BlocksB) < 2:
				c.Reason = "处理 " + b + " 仅出现在单一区组，不使用空间平滑伪造重复"
			case !estimable:
				c.Reason = "该处理在此分支无有效观测"
			default:
				c.Reason = "可估：跨 " + strconv.Itoa(len(c.BlocksA)) + "/" +
					strconv.Itoa(len(c.BlocksB)) + " 个区组"
			}
			c.RawDiff = round6(rawMean[a] - rawMean[b])
			if estimable {
				c.AdjustedDiff = round6(am.mean - bm.mean)
				c.CorrectionDelta = round6(c.AdjustedDiff - c.RawDiff)
				c.StandardError = round6(contrastSE(entries, os, model, a, b, treatments))
			}
			res.Contrasts = append(res.Contrasts, c)
		}
	}
	return res
}
