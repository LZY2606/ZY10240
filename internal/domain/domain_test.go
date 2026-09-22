package domain

import (
	"testing"
)

func TestAuditBlocksDuplicateCoordinatesAndUnits(t *testing.T) {
	plots := []Plot{
		{ID: "P1", Block: "B1", Row: 1, Col: 1, Line: "L1", Treatment: "T1"},
		{ID: "P2", Block: "B1", Row: 1, Col: 1, Line: "L2", Treatment: "T2"},
		{ID: "P3", Block: "B2", Row: 2, Col: 1, Line: "L3", Treatment: "T1"},
	}
	r := AuditDesign(plots)
	if r.OK {
		t.Fatal("重复坐标必须使设计检查不通过")
	}
	var sawDup bool
	for _, c := range r.Conflicts {
		if c.Kind == "duplicate_coordinate" && c.Key == "1,1" {
			sawDup = true
		}
	}
	if !sawDup {
		t.Fatalf("期望重复坐标冲突，得到 %+v", r.Conflicts)
	}
}

func TestAuditRejectsUnitWithTwoLines(t *testing.T) {
	plots := []Plot{
		{ID: "P1", Block: "B1", Row: 1, Col: 1, Line: "L1", Treatment: "T1"},
		{ID: "P1", Block: "B1", Row: 1, Col: 2, Line: "L9", Treatment: "T1"},
	}
	r := AuditDesign(plots)
	if r.OK {
		t.Fatal("同一单元两个品系必须阻止分析")
	}
	var found bool
	for _, c := range r.Conflicts {
		if c.Kind == "unit_two_lines" {
			found = true
		}
	}
	if !found {
		t.Fatalf("期望 unit_two_lines，得到 %+v", r.Conflicts)
	}
}

func TestSingleBlockTreatmentContrastNotEstimable(t *testing.T) {
	plots := []Plot{
		{ID: "a1", Block: "B1", Row: 1, Col: 1, Line: "A", Treatment: "T1"},
		{ID: "a2", Block: "B2", Row: 2, Col: 1, Line: "A", Treatment: "T1"},
		{ID: "a3", Block: "B3", Row: 3, Col: 1, Line: "A", Treatment: "T1"},
		{ID: "b1", Block: "B2", Row: 2, Col: 2, Line: "B", Treatment: "T2"},
		{ID: "b2", Block: "B3", Row: 3, Col: 2, Line: "B", Treatment: "T2"},
		{ID: "c1", Block: "B1", Row: 1, Col: 3, Line: "C", Treatment: "T3"},
		{ID: "c2", Block: "B1", Row: 1, Col: 4, Line: "C", Treatment: "T3"},
	}
	r := AuditDesign(plots)
	if !r.OK {
		t.Fatalf("该布局无坐标冲突: %+v", r.Conflicts)
	}
	inPairs := func(pairs [][2]string, a, b string) bool {
		for _, p := range pairs {
			if p[0] == a && p[1] == b {
				return true
			}
		}
		return false
	}
	if !inPairs(r.NonEstimablePairs, "T1", "T3") || !inPairs(r.NonEstimablePairs, "T2", "T3") {
		t.Fatalf("T3 单区组对比必须不可估: %+v", r.NonEstimablePairs)
	}
	if !inPairs(r.EstimablePairs, "T1", "T2") {
		t.Fatalf("T1-T2 应可估: %+v", r.EstimablePairs)
	}
}

func TestFitMissingObservationNotZeroAndDeterministic(t *testing.T) {
	mk := func() []Plot {
		var ps []Plot
		trts := []string{"T1", "T2", "T3", "T4"}
		id := 0
		for r := 1; r <= 4; r++ {
			for c := 1; c <= 4; c++ {
				id++
				pid := string(rune('a'+id-1)) + "x"
				tr := trts[(r+c)%4]
				ps = append(ps, Plot{ID: pid, Block: []string{"B1", "B1", "B2", "B2"}[r-1],
					Row: r, Col: c, Line: "L" + pid, Treatment: tr,
					Yield: 10 + float64(r)*0.5 - float64(c)*0.25 + float64((r+c)%3), YieldNA: false})
			}
		}
		return ps
	}
	plots := mk()
	// make one interior plot missing
	for i := range plots {
		if plots[i].Row == 2 && plots[i].Col == 2 {
			plots[i].YieldNA = true
			plots[i].Yield = 0
		}
	}
	f1 := Fit(ModelRowCol, "yield", false, plots)
	f2 := Fit(ModelRowCol, "yield", false, plots)
	if len(f1.Plots) != len(plots) {
		t.Fatalf("缺区仍应出现在结果中: %d", len(f1.Plots))
	}
	for _, p := range f1.Plots {
		if p.PlotID == "fx" {
			if !p.RawNA {
				t.Fatal("缺区标记必须保留")
			}
			if p.Fitted != 0 || p.Residual != 0 || p.Adjusted != 0 {
				t.Fatalf("缺区不得按零参与拟合: %+v", p)
			}
		}
	}
	if resultsDiffer(f1, f2) {
		t.Fatal("同一快照重复拟合必须完全一致（可重放）")
	}
}

func TestLocalModelUsesNoWraparoundAtEdge(t *testing.T) {
	// 3x3 grid: corner plot must only see its two real orthogonal neighbours.
	plots := []Plot{
		{ID: "c1", Block: "B1", Row: 1, Col: 1, Line: "L1", Treatment: "T1", Yield: 10},
		{ID: "c2", Block: "B1", Row: 1, Col: 2, Line: "L2", Treatment: "T2", Yield: 20},
		{ID: "c3", Block: "B1", Row: 1, Col: 3, Line: "L3", Treatment: "T3", Yield: 30},
		{ID: "c4", Block: "B1", Row: 2, Col: 1, Line: "L4", Treatment: "T2", Yield: 40},
		{ID: "c5", Block: "B1", Row: 2, Col: 2, Line: "L5", Treatment: "T3", Yield: 50},
		{ID: "c6", Block: "B1", Row: 2, Col: 3, Line: "L6", Treatment: "T1", Yield: 60},
		{ID: "c7", Block: "B2", Row: 3, Col: 1, Line: "L7", Treatment: "T3", Yield: 70},
		{ID: "c8", Block: "B2", Row: 3, Col: 2, Line: "L8", Treatment: "T1", Yield: 80},
		{ID: "c9", Block: "B2", Row: 3, Col: 3, Line: "L9", Treatment: "T2", Yield: 90},
	}
	r := AuditDesign(plots)
	entries := make([]entryState, len(plots))
	for i, p := range plots {
		entries[i].p = p
		entries[i].used = true
	}
	g := newNeighbourGrid(entries)
	// initialize residuals = raw-y - 50
	for i := range entries {
		entries[i].resid = entries[i].p.Yield - 50
	}
	v, n := g.covariate(entries, 1, 1)
	if n != 2 {
		t.Fatalf("角落地块只应有 2 个真实邻居，得到 %d（不得环绕）", n)
	}
	// neighbours are (1,2)=-30 and (2,1)=-10 -> mean -20
	if v != -20 {
		t.Fatalf("角落邻居残差均值应为 -20，得到 %v", v)
	}
	v2, n2 := g.covariate(entries, 2, 2)
	if n2 != 4 {
		t.Fatalf("内部地块应有 4 个邻居，得到 %d", n2)
	}
	_ = v2
	_ = Fit(ModelLocal, "yield", false, plots)
	_ = r
}

func TestResultsEqualReplayAcrossModels(t *testing.T) {
	plots := []Plot{
		{ID: "p1", Block: "B1", Row: 1, Col: 1, Line: "L1", Treatment: "T1", Yield: 12},
		{ID: "p2", Block: "B1", Row: 1, Col: 2, Line: "L2", Treatment: "T2", Yield: 9},
		{ID: "p3", Block: "B2", Row: 2, Col: 1, Line: "L3", Treatment: "T2", Yield: 14},
		{ID: "p4", Block: "B2", Row: 2, Col: 2, Line: "L4", Treatment: "T1", Yield: 11},
		{ID: "p5", Block: "B3", Row: 3, Col: 1, Line: "L5", Treatment: "T1", Yield: 13},
		{ID: "p6", Block: "B3", Row: 3, Col: 2, Line: "L6", Treatment: "T2", Yield: 10},
	}
	for _, m := range []string{ModelRowCol, ModelBlock, ModelLocal} {
		a := Fit(m, "yield", false, plots)
		b := Fit(m, "yield", false, plots)
		if resultsDiffer(a, b) {
			t.Fatalf("模型 %s 重放不一致", m)
		}
		if !a.Contrasts[0].Estimable {
			t.Fatalf("模型 %s 跨3区组对比应可估", m)
		}
	}
}

func resultsDiffer(a, b FitResult) bool {
	if len(a.Plots) != len(b.Plots) {
		return true
	}
	for i := range a.Plots {
		if a.Plots[i] != b.Plots[i] {
			return true
		}
	}
	return false
}
