package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"fieldbench/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *Service) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)
	if err := svc.EnsureSeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(svc).Handler())
	t.Cleanup(srv.Close)
	return srv, svc
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET %s -> %d", url, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func postJSON(t *testing.T, url string, body string) map[string]any {
	t.Helper()
	res, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode >= 400 {
		t.Fatalf("POST %s -> %d %v", url, res.StatusCode, out)
	}
	return out
}

func TestAcceptanceFlow(t *testing.T) {
	srv, _ := newTestServer(t)

	// 1. page title
	res, _ := http.Get(srv.URL + "/")
	buf := make([]byte, 4096)
	n, _ := res.Body.Read(buf)
	res.Body.Close()
	if !strings.Contains(string(buf[:n]), "田区空间校正台") {
		t.Fatal("首页必须显示“田区空间校正台”")
	}

	// 2. initial state: duplicate blocks analysis
	var state State
	getJSON(t, srv.URL+"/api/state", &state)
	if state.Design.OK {
		t.Fatal("fixture 初始含重复坐标，必须阻止分析")
	}
	var hasDup bool
	for _, c := range state.Design.Conflicts {
		if c.Kind == "duplicate_coordinate" && c.Key == "7,1" {
			hasDup = true
		}
	}
	if !hasDup {
		t.Fatalf("期望 (7,1) 重复坐标冲突，得到 %+v", state.Design.Conflicts)
	}

	// 3. publishing must be blocked and recorded
	out := postJSON(t, srv.URL+"/api/branches",
		`{"model":"rowcol","trait":"yield"}`)
	if out["status"] != "blocked" {
		t.Fatalf("冲突未修正时分支必须 blocked，得到 %v", out["status"])
	}

	// 4. fix P299 coordinate to the empty (9,6)
	postJSON(t, srv.URL+"/api/plots/P299/coordinates", `{"row":9,"col":6}`)
	getJSON(t, srv.URL+"/api/state", &state)
	if !state.Design.OK {
		t.Fatalf("修正坐标后设计应通过: %+v", state.Design.Conflicts)
	}

	// 5. publish all three models; T4 contrasts remain non-estimable
	for _, m := range []string{"rowcol", "block", "local"} {
		b := postJSON(t, srv.URL+"/api/branches",
			`{"model":"`+m+`","trait":"yield"}`)
		if b["status"] != "published" {
			t.Fatalf("模型 %s 应发布", m)
		}
	}
	getJSON(t, srv.URL+"/api/state", &state)
	if len(state.Branches) < 4 {
		t.Fatalf("期望含被阻止分支和 3 个已发布分支，得到 %d", len(state.Branches))
	}
	var pub store.Branch
	getJSON(t, srv.URL+"/api/branches/2", &pub)
	if pub.Status != "published" {
		t.Fatalf("分支2应为已发布，实际 %s", pub.Status)
	}
	foundNonEst := false
	for _, c := range pub.Result.Contrasts {
		if (c.Pair[0] == "T4" || c.Pair[1] == "T4") && c.Estimable {
			t.Fatalf("T4 仅在 B1，其对比不得可估: %+v", c)
		}
		if c.Pair[0] == "T1" && c.Pair[1] == "T4" && !c.Estimable {
			foundNonEst = true
			if c.AdjustedDiff != 0 {
				t.Fatal("不可估对比不得给出伪造的校正差")
			}
		}
		if c.Estimable && (len(c.BlocksA) < 2 || len(c.BlocksB) < 2) {
			t.Fatal("可估对比必须绑定至少两个区组")
		}
	}
	if !foundNonEst {
		t.Fatal("未找到 T1-T4 不可估对比")
	}

	// 6. missing plot carried through as missing, not zero
	var missCount int
	for _, p := range pub.Result.Plots {
		if p.PlotID == "P021" {
			if !p.RawNA || p.Adjusted != 0 || p.Fitted != 0 {
				t.Fatalf("缺区 P021 不得按零参与: %+v", p)
			}
			missCount++
		}
	}
	if missCount != 1 {
		t.Fatalf("缺区记录数错误: %d", missCount)
	}

	// 7. edge no wrap: SVG renders and states rule
	svgRes, err := http.Get(srv.URL + "/api/branches/2/grid.svg?layer=residual")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, svgRes)
	svgRes.Body.Close()
	if svgRes.StatusCode != 200 || !strings.Contains(body, "<svg") {
		t.Fatal("SVG 网格必须可访问")
	}
	if !strings.Contains(body, "不使用环绕邻居") {
		t.Fatal("图例必须声明不使用环绕邻居")
	}

	// 8. replay matches
	rep := postJSON(t, srv.URL+"/api/branches/2/replay", "")
	if rep["match"] != true {
		t.Fatalf("重放必须与发布结果一致: %v", rep["match"])
	}

	// 9. runs export
	runs, err := http.Get(srv.URL + "/api/export/runs.csv")
	if err != nil {
		t.Fatal(err)
	}
	runsBody := readAll(t, runs)
	runs.Body.Close()
	if !strings.Contains(runsBody, "branch_blocked") ||
		!strings.Contains(runsBody, "branch_published") ||
		!strings.Contains(runsBody, "coordinate_fix") {
		t.Fatalf("运行记录必须包含阻止/发布/修正: %s", runsBody)
	}

	// 10. branch CSV includes contrasts and coverage
	cr, err := http.Get(srv.URL + "/api/branches/2/export.csv")
	if err != nil {
		t.Fatal(err)
	}
	cb := readAll(t, cr)
	cr.Body.Close()
	if !strings.Contains(cb, "contrast") || !strings.Contains(cb, "T1--T4") {
		t.Fatalf("分支导出必须包含对比与覆盖: %s", cb)
	}

	// 11. reset and re-verify
	postJSON(t, srv.URL+"/api/reset", "")
	getJSON(t, srv.URL+"/api/state", &state)
	if state.Design.OK {
		t.Fatal("清空重导后应恢复重复坐标阻止状态")
	}
	if len(state.Branches) != 0 {
		t.Fatalf("清空后不应保留旧分支，得到 %d", len(state.Branches))
	}
}

func TestEdgeExclusionBranch(t *testing.T) {
	srv, _ := newTestServer(t)
	postJSON(t, srv.URL+"/api/plots/P299/coordinates", `{"row":9,"col":6}`)
	b := postJSON(t, srv.URL+"/api/branches",
		`{"model":"local","trait":"yield","exclude_edge":true}`)
	id := int64(b["id"].(float64))
	var br store.Branch
	getJSON(t, srv.URL+"/api/branches/"+strconv.FormatInt(id, 10), &br)
	if !br.Result.ExcludeEdge {
		t.Fatal("分支必须记录排除边缘")
	}
	for _, p := range br.Result.Plots {
		if p.Edge {
			if p.Fitted != 0 || p.Adjusted != 0 {
				t.Fatalf("排除边缘后边缘地块不得参与拟合: %+v", p)
			}
			continue
		}
		if !p.Excluded && !p.RawNA && p.Fitted == 0 {
			t.Fatalf("内部地块应参与拟合: %+v", p)
		}
	}
}

func readAll(t *testing.T, r *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
