// Package httpapi exposes the field workbench over HTTP and renders the
// SVG field grid.
package httpapi

import (
	"context"
	"fmt"
	"time"

	"fieldbench/internal/domain"
	"fieldbench/internal/fixtures"
	"fieldbench/internal/store"
)

// Service coordinates storage, the design audit and model fitting.
type Service struct {
	Store *store.Store
	Now   func() time.Time
}

// New constructs a service.
func New(st *store.Store) *Service {
	return &Service{Store: st, Now: time.Now}
}

func (s *Service) stamp() string { return s.Now().UTC().Format(time.RFC3339) }

// Reimport replaces the trial with the fixed, embedded fixture.
func (s *Service) Reimport(ctx context.Context) ([]domain.Plot, error) {
	plots, err := fixtures.Load()
	if err != nil {
		return nil, err
	}
	if err := s.Store.ReplacePlots(ctx, plots); err != nil {
		return nil, err
	}
	_ = s.Store.AddAudit(ctx, s.stamp(), "reimport",
		fmt.Sprintf("从固定 fixture 重新导入 %d 条地块记录", len(plots)))
	return plots, nil
}

// EnsureSeeded imports the fixture once on an empty database.
func (s *Service) EnsureSeeded(ctx context.Context) error {
	n, err := s.Store.Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.Reimport(ctx)
	return err
}

// State is the operational payload for the workbench page.
type State struct {
	Plots     []domain.Plot       `json:"plots"`
	Design    domain.DesignReport `json:"design"`
	Traits    []domain.Trait      `json:"traits"`
	Revisions []store.Revision    `json:"revisions"`
	Branches  []store.Branch      `json:"branches"`
	Audit     []store.AuditEntry  `json:"audit"`
}

// Snapshot returns the full operational state.
func (s *Service) Snapshot(ctx context.Context) (State, error) {
	plots, err := s.Store.ListPlots(ctx)
	if err != nil {
		return State{}, err
	}
	revs, err := s.Store.ListRevisions(ctx)
	if err != nil {
		return State{}, err
	}
	branches, err := s.Store.ListBranches(ctx)
	if err != nil {
		return State{}, err
	}
	audit, err := s.Store.ListAudit(ctx)
	if err != nil {
		return State{}, err
	}
	return State{
		Plots: plots, Design: domain.AuditDesign(plots),
		Traits: domain.Traits, Revisions: revs,
		Branches: branches, Audit: audit,
	}, nil
}

// PublishBranch validates the design, then either rejects the branch or fits
// the selected model on a frozen plot snapshot.
func (s *Service) PublishBranch(ctx context.Context, name, model, trait string, excludeEdge bool) (store.Branch, error) {
	switch model {
	case domain.ModelRowCol, domain.ModelBlock, domain.ModelLocal:
	default:
		return store.Branch{}, fmt.Errorf("未知模型 %q", model)
	}
	if trait != "yield" && trait != "height" {
		return store.Branch{}, fmt.Errorf("未知性状 %q", trait)
	}
	if name == "" {
		name = domain.ModelName(model) + "-" + s.stamp()
	}
	plots, err := s.Store.ListPlots(ctx)
	if err != nil {
		return store.Branch{}, err
	}
	b := store.Branch{
		Name: name, Model: model, Trait: trait,
		ExcludeEdge: excludeEdge, CreatedAt: s.stamp(),
		Snapshot: append([]domain.Plot(nil), plots...),
	}
	report := domain.AuditDesign(plots)
	if !report.OK {
		b.Status = "blocked"
		b.Reason = fmt.Sprintf("存在 %d 个重复/冲突单元，必须先修正坐标；不按最后导入覆盖", len(report.Conflicts))
		id, err := s.Store.CreateBranch(ctx, &b)
		if err != nil {
			return store.Branch{}, err
		}
		_ = s.Store.AddAudit(ctx, s.stamp(), "branch_blocked",
			fmt.Sprintf("分支 #%d 因冲突被阻止: %s", id, b.Reason))
		return s.Store.GetBranch(ctx, id)
	}
	b.Result = domain.Fit(model, trait, excludeEdge, append([]domain.Plot(nil), plots...))
	b.Status = "published"
	id, err := s.Store.CreateBranch(ctx, &b)
	if err != nil {
		return store.Branch{}, err
	}
	_ = s.Store.AddAudit(ctx, s.stamp(), "branch_published",
		fmt.Sprintf("发布分支 #%d: %s / %s / 排除边缘=%v", id, domain.ModelName(model), trait, excludeEdge))
	return s.Store.GetBranch(ctx, id)
}

// Replay refits a stored branch from its frozen snapshot and reports whether
// the recomputation matches the persisted result exactly.
func Replay(b store.Branch) (domain.FitResult, bool) {
	if b.Status != "published" {
		return domain.FitResult{}, false
	}
	got := domain.Fit(b.Model, b.Trait, b.ExcludeEdge, b.Snapshot)
	return got, resultsEqual(got, b.Result)
}

func resultsEqual(a, b2 domain.FitResult) bool {
	if a.Model != b2.Model || a.Trait != b2.Trait || a.ExcludeEdge != b2.ExcludeEdge {
		return false
	}
	if len(a.Plots) != len(b2.Plots) || len(a.Contrasts) != len(b2.Contrasts) {
		return false
	}
	for i := range a.Plots {
		if a.Plots[i] != b2.Plots[i] {
			return false
		}
	}
	for i := range a.Contrasts {
		x, y := a.Contrasts[i], b2.Contrasts[i]
		if x.Pair != y.Pair || x.Estimable != y.Estimable || x.Reason != y.Reason ||
			!equalStrings(x.BlocksA, y.BlocksA) || !equalStrings(x.BlocksB, y.BlocksB) ||
			x.NAdjustedA != y.NAdjustedA || x.NAdjustedB != y.NAdjustedB ||
			x.RawDiff != y.RawDiff || x.AdjustedDiff != y.AdjustedDiff ||
			x.CorrectionDelta != y.CorrectionDelta || x.StandardError != y.StandardError {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
