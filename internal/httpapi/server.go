package httpapi

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fieldbench/internal/domain"
	"fieldbench/internal/fixtures"
	"fieldbench/internal/store"
)

//go:embed static/index.html
var indexHTML string

// Server wires the service to HTTP routes.
type Server struct {
	svc *Service
}

// NewServer builds the HTTP handler.
func NewServer(svc *Service) *Server { return &Server{svc: svc} }

// Handler returns the root mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/reimport", s.handleReimport)
	mux.HandleFunc("POST /api/plots/{id}/coordinates", s.handleCoordinates)
	mux.HandleFunc("POST /api/plots/{id}/exclude", s.handleExclude)
	mux.HandleFunc("POST /api/branches", s.handleCreateBranch)
	mux.HandleFunc("GET /api/branches/{id}", s.handleGetBranch)
	mux.HandleFunc("POST /api/branches/{id}/replay", s.handleReplay)
	mux.HandleFunc("GET /api/branches/{id}/grid.svg", s.handleBranchGrid)
	mux.HandleFunc("GET /api/grid.svg", s.handleGrid)
	mux.HandleFunc("GET /api/export/runs.csv", s.handleExportRuns)
	mux.HandleFunc("GET /api/branches/{id}/export.csv", s.handleExportBranch)
	mux.HandleFunc("GET /api/fixture", s.handleFixture)
	mux.HandleFunc("POST /api/reset", s.handleReset)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

func writeJSON(w http.ResponseWriter, v any, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, map[string]string{"error": msg}, code)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.Snapshot(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, st, 200)
}

func (s *Server) handleReimport(w http.ResponseWriter, r *http.Request) {
	plots, err := s.svc.Reimport(r.Context())
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"imported": len(plots)}, 200)
}

type coordReq struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

func (s *Server) handleCoordinates(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req coordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "请求体解析失败: "+err.Error())
		return
	}
	if req.Row < 1 || req.Col < 1 {
		writeError(w, 400, "行列号必须为正整数")
		return
	}
	rev, err := s.svc.Store.UpdateCoordinates(r.Context(), id, req.Row, req.Col,
		s.svc.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	_ = s.svc.Store.AddAudit(r.Context(), s.svc.Now().UTC().Format(time.RFC3339),
		"coordinate_fix",
		fmt.Sprintf("修正 %s: (%d,%d) -> (%d,%d)", id, rev.OldRow, rev.OldCol, rev.NewRow, rev.NewCol))
	writeJSON(w, rev, 200)
}

type excludeReq struct {
	Excluded bool `json:"excluded"`
}

func (s *Server) handleExclude(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req excludeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := s.svc.Store.SetExcluded(r.Context(), id, req.Excluded); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	_ = s.svc.Store.AddAudit(r.Context(), s.svc.Now().UTC().Format(time.RFC3339),
		"exclude_plot", fmt.Sprintf("%s 排除=%v", id, req.Excluded))
	writeJSON(w, map[string]bool{"ok": true}, 200)
}

type branchReq struct {
	Name        string `json:"name"`
	Model       string `json:"model"`
	Trait       string `json:"trait"`
	ExcludeEdge bool   `json:"exclude_edge"`
}

func (s *Server) handleCreateBranch(w http.ResponseWriter, r *http.Request) {
	var req branchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	b, err := s.svc.PublishBranch(r.Context(), req.Name, req.Model, req.Trait, req.ExcludeEdge)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, b, 200)
}

func (s *Server) handleGetBranch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "分支 id 非法")
		return
	}
	b, err := s.svc.Store.GetBranch(r.Context(), id)
	if err == store.ErrNotFound {
		writeError(w, 404, "分支不存在")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, b, 200)
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "分支 id 非法")
		return
	}
	b, err := s.svc.Store.GetBranch(r.Context(), id)
	if err == store.ErrNotFound {
		writeError(w, 404, "分支不存在")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	got, match := Replay(b)
	_ = s.svc.Store.AddAudit(r.Context(), s.svc.Now().UTC().Format(time.RFC3339),
		"replay", fmt.Sprintf("重放分支 #%d，结果一致=%v", id, match))
	writeJSON(w, map[string]any{"match": match, "result": got}, 200)
}

func (s *Server) gridContext(r *http.Request) ([]domain.Plot, domain.DesignReport, *store.Branch, string) {
	plots, _ := s.svc.Store.ListPlots(r.Context())
	design := domain.AuditDesign(plots)
	trait := r.URL.Query().Get("trait")
	if trait == "" {
		trait = "yield"
	}
	var branch *store.Branch
	if idStr := r.PathValue("id"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			if b, err := s.svc.Store.GetBranch(r.Context(), id); err == nil {
				branch = &b
				plots = b.Snapshot
				design = domain.AuditDesign(plots)
				trait = b.Trait
			}
		}
	}
	return plots, design, branch, trait
}

func (s *Server) writeGrid(w http.ResponseWriter, r *http.Request) {
	plots, design, branch, trait := s.gridContext(r)
	layer := r.URL.Query().Get("layer")
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	_ = RenderGrid(w, plots, design, branch, trait, layer)
}

func (s *Server) handleGrid(w http.ResponseWriter, r *http.Request)       { s.writeGrid(w, r) }
func (s *Server) handleBranchGrid(w http.ResponseWriter, r *http.Request) { s.writeGrid(w, r) }

func (s *Server) handleExportRuns(w http.ResponseWriter, r *http.Request) {
	entries, err := s.svc.Store.ListAudit(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="runs.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "at", "action", "detail"})
	for _, e := range entries {
		_ = cw.Write([]string{strconv.FormatInt(e.ID, 10), e.At, e.Action, e.Detail})
	}
	cw.Flush()
}

func (s *Server) handleExportBranch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "分支 id 非法")
		return
	}
	b, err := s.svc.Store.GetBranch(r.Context(), id)
	if err == store.ErrNotFound {
		writeError(w, 404, "分支不存在")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="branch-%d.csv"`, id))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"kind", "name", "plot_id", "block", "row", "col",
		"line", "treatment", "raw", "fitted", "residual", "adjusted",
		"raw_na", "excluded", "edge", "estimable", "blocks_a", "blocks_b",
		"raw_diff", "adjusted_diff", "correction_delta", "standard_error"})
	for _, p := range b.Result.Plots {
		_ = cw.Write([]string{
			"plot", b.Name, p.PlotID, p.Block, strconv.Itoa(p.Row), strconv.Itoa(p.Col),
			p.Line, p.Treatment, fnum(p.Raw, p.RawNA), fnum(p.Fitted, p.RawNA),
			fnum(p.Residual, p.RawNA), fnum(p.Adjusted, p.RawNA),
			strconv.FormatBool(p.RawNA), strconv.FormatBool(p.Excluded), strconv.FormatBool(p.Edge),
			"", "", "", "", "", "", "",
		})
	}
	for _, c := range b.Result.Contrasts {
		_ = cw.Write([]string{
			"contrast", b.Name, "", "", "", "", "",
			c.Pair[0] + "--" + c.Pair[1], "", "", "", "",
			"", "", "", strconv.FormatBool(c.Estimable),
			strings.Join(c.BlocksA, "|"), strings.Join(c.BlocksB, "|"),
			strconv.FormatFloat(c.RawDiff, 'f', 6, 64),
			strconv.FormatFloat(c.AdjustedDiff, 'f', 6, 64),
			strconv.FormatFloat(c.CorrectionDelta, 'f', 6, 64),
			strconv.FormatFloat(c.StandardError, 'f', 6, 64),
		})
	}
	cw.Flush()
}

func fnum(v float64, na bool) string {
	if na {
		return ""
	}
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func (s *Server) handleFixture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	fmt.Fprint(w, fixtures.TrialCSV)
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	plots, err := s.svc.Reimport(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	_ = s.svc.Store.AddAudit(r.Context(), s.svc.Now().UTC().Format(time.RFC3339),
		"reset", "清空数据库并重新导入固定 fixture 复核")
	writeJSON(w, map[string]any{"reset": true, "imported": len(plots)}, 200)
}
