package store

import (
	"context"
	"errors"
)

// ErrNotFound signals a missing row.
var ErrNotFound = errors.New("记录不存在")

// AuditEntry is one exportable run/operation record.
type AuditEntry struct {
	ID     int64  `json:"id"`
	At     string `json:"at"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

// AddAudit appends an operation record to the run log.
func (s *Store) AddAudit(ctx context.Context, at, action, detail string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log(at,action,detail) VALUES (?,?,?)`, at, action, detail)
	return err
}

// ListAudit returns the full run log oldest first.
func (s *Store) ListAudit(ctx context.Context) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,at,action,detail FROM audit_log ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Action, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Count returns the number of imported plots (including duplicated
// coordinates, which are intentionally retained for the blocking audit).
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plots`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
