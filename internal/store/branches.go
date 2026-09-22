package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"fieldbench/internal/domain"
)

// Branch is one published spatial-correction analysis branch.
type Branch struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	Model       string           `json:"model"`
	Trait       string           `json:"trait"`
	ExcludeEdge bool             `json:"exclude_edge"`
	Status      string           `json:"status"`
	Reason      string           `json:"reason"`
	Snapshot    []domain.Plot    `json:"snapshot"`
	Result      domain.FitResult `json:"result"`
	CreatedAt   string           `json:"created_at"`
}

type branchRow struct {
	id          int64
	name        string
	model       string
	trait       string
	excludeEdge int
	status      string
	reason      string
	snapshot    string
	result      string
	createdAt   string
}

// CreateBranch persists a branch with its frozen snapshot and result.
func (s *Store) CreateBranch(ctx context.Context, b *Branch) (int64, error) {
	snap, err := json.Marshal(b.Snapshot)
	if err != nil {
		return 0, err
	}
	resJSON := ""
	if b.Status == "published" {
		raw, err := json.Marshal(b.Result)
		if err != nil {
			return 0, err
		}
		resJSON = string(raw)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO branches
		(name,model,trait,exclude_edge,status,reason,snapshot,result,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		b.Name, b.Model, b.Trait, boolInt(b.ExcludeEdge), b.Status, b.Reason,
		string(snap), resJSON, b.CreatedAt)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	b.ID = id
	return id, nil
}

func scanBranchRow(row interface {
	Scan(dest ...any) error
}) (branchRow, error) {
	var r branchRow
	err := row.Scan(&r.id, &r.name, &r.model, &r.trait, &r.excludeEdge,
		&r.status, &r.reason, &r.snapshot, &r.result, &r.createdAt)
	return r, err
}

func decodeBranch(r branchRow) (Branch, error) {
	b := Branch{
		ID: r.id, Name: r.name, Model: r.model, Trait: r.trait,
		ExcludeEdge: r.excludeEdge == 1, Status: r.status, Reason: r.reason,
		CreatedAt: r.createdAt,
	}
	if err := json.Unmarshal([]byte(r.snapshot), &b.Snapshot); err != nil {
		return b, err
	}
	if r.result != "" {
		if err := json.Unmarshal([]byte(r.result), &b.Result); err != nil {
			return b, err
		}
	}
	return b, nil
}

// ListBranches returns all branches newest first.
func (s *Store) ListBranches(ctx context.Context) ([]Branch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		id,name,model,trait,exclude_edge,status,reason,snapshot,result,created_at
		FROM branches ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Branch
	for rows.Next() {
		r, err := scanBranchRow(rows)
		if err != nil {
			return nil, err
		}
		b, err := decodeBranch(r)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// GetBranch loads one branch.
func (s *Store) GetBranch(ctx context.Context, id int64) (Branch, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id,name,model,trait,exclude_edge,status,reason,snapshot,result,created_at
		FROM branches WHERE id=?`, id)
	r, err := scanBranchRow(row)
	if err == sql.ErrNoRows {
		return Branch{}, ErrNotFound
	}
	if err != nil {
		return Branch{}, err
	}
	return decodeBranch(r)
}
