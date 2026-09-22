// Package store persists plots, corrections, analysis branches and an
// append-only run/audit log in SQLite.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"fieldbench/internal/domain"

	_ "modernc.org/sqlite"
)

// Store is the application data gateway.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at dsn.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS plots (
  plot_id   TEXT NOT NULL,
  block     TEXT NOT NULL,
  row_no    INTEGER NOT NULL,
  col_no    INTEGER NOT NULL,
  line_name TEXT NOT NULL,
  treatment TEXT NOT NULL,
  yield     REAL,
  height    REAL,
  excluded  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (plot_id)
);
CREATE TABLE IF NOT EXISTS plot_revisions (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  plot_id    TEXT NOT NULL,
  old_row    INTEGER NOT NULL,
  old_col    INTEGER NOT NULL,
  new_row    INTEGER NOT NULL,
  new_col    INTEGER NOT NULL,
  changed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS branches (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL,
  model         TEXT NOT NULL,
  trait         TEXT NOT NULL,
  exclude_edge  INTEGER NOT NULL,
  status        TEXT NOT NULL,
  reason        TEXT NOT NULL DEFAULT '',
  snapshot      TEXT NOT NULL,
  result        TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  at         TEXT NOT NULL,
  action     TEXT NOT NULL,
  detail     TEXT NOT NULL
);
`

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// scanPlots loads all rows and flags edge plots from the bounding box.
func scanPlots(rows *sql.Rows) ([]domain.Plot, error) {
	var plots []domain.Plot
	for rows.Next() {
		var p domain.Plot
		var y, h sql.NullFloat64
		var ex int
		if err := rows.Scan(&p.ID, &p.Block, &p.Row, &p.Col, &p.Line,
			&p.Treatment, &y, &h, &ex); err != nil {
			return nil, err
		}
		p.YieldNA, p.Yield = !y.Valid, y.Float64
		p.HeightNA, p.Height = !h.Valid, h.Float64
		p.Excluded = ex == 1
		plots = append(plots, p)
	}
	if len(plots) > 0 {
		minR, maxR, minC, maxC := plots[0].Row, plots[0].Row, plots[0].Col, plots[0].Col
		for _, p := range plots {
			minR = minInt(minR, p.Row)
			maxR = maxInt(maxR, p.Row)
			minC = minInt(minC, p.Col)
			maxC = maxInt(maxC, p.Col)
		}
		for i := range plots {
			p := &plots[i]
			p.Edge = p.Row == minR || p.Row == maxR || p.Col == minC || p.Col == maxC
		}
	}
	return plots, nil
}

const selectPlots = `SELECT plot_id,block,row_no,col_no,line_name,treatment,
	yield,height,excluded FROM plots`

// ListPlots returns every imported plot with edge flags derived.
func (s *Store) ListPlots(ctx context.Context) ([]domain.Plot, error) {
	rows, err := s.db.QueryContext(ctx, selectPlots+` ORDER BY row_no,col_no,plot_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlots(rows)
}

// ReplacePlots performs a full re-import. Plot id is the unique experimental
// unit key; a repeated unit fails the insert rather than letting the last
// import win. Duplicate coordinates (different ids) are retained so the
// design audit can block analysis on them.
func (s *Store) ReplacePlots(ctx context.Context, plots []domain.Plot) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"plot_revisions", "branches", "audit_log", "plots"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return err
		}
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO plots
		(plot_id,block,row_no,col_no,line_name,treatment,yield,height,excluded)
		VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range plots {
		var y, h sql.NullFloat64
		if !p.YieldNA {
			y = sql.NullFloat64{Float64: p.Yield, Valid: true}
		}
		if !p.HeightNA {
			h = sql.NullFloat64{Float64: p.Height, Valid: true}
		}
		if _, err := stmt.ExecContext(ctx, p.ID, p.Block, p.Row, p.Col,
			p.Line, p.Treatment, y, h, boolInt(p.Excluded)); err != nil {
			return fmt.Errorf("试验单元 %s 重复，拒绝按最后导入覆盖: %w", p.ID, err)
		}
	}
	return tx.Commit()
}

// Revision records one coordinate correction.
type Revision struct {
	ID        int64  `json:"id"`
	PlotID    string `json:"plot_id"`
	OldRow    int    `json:"old_row"`
	OldCol    int    `json:"old_col"`
	NewRow    int    `json:"new_row"`
	NewCol    int    `json:"new_col"`
	ChangedAt string `json:"changed_at"`
}

// ListRevisions returns all coordinate corrections newest first.
func (s *Store) ListRevisions(ctx context.Context) ([]Revision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,plot_id,old_row,old_col,new_row,new_col,changed_at
		FROM plot_revisions ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.PlotID, &r.OldRow, &r.OldCol,
			&r.NewRow, &r.NewCol, &r.ChangedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// UpdateCoordinates corrects one plot's row/col.
func (s *Store) UpdateCoordinates(ctx context.Context, plotID string, newRow, newCol int, at string) (Revision, error) {
	var oldRow, oldCol int
	err := s.db.QueryRowContext(ctx,
		`SELECT row_no,col_no FROM plots WHERE plot_id=?`, plotID).Scan(&oldRow, &oldCol)
	if err == sql.ErrNoRows {
		return Revision{}, fmt.Errorf("地块 %s 不存在", plotID)
	}
	if err != nil {
		return Revision{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE plots SET row_no=?,col_no=? WHERE plot_id=?`, newRow, newCol, plotID); err != nil {
		return Revision{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO plot_revisions
		(plot_id,old_row,old_col,new_row,new_col,changed_at) VALUES (?,?,?,?,?,?)`,
		plotID, oldRow, oldCol, newRow, newCol, at)
	if err != nil {
		return Revision{}, err
	}
	id, _ := res.LastInsertId()
	return Revision{ID: id, PlotID: plotID, OldRow: oldRow, OldCol: oldCol,
		NewRow: newRow, NewCol: newCol, ChangedAt: at}, nil
}

// SetExcluded toggles manual exclusion of one plot.
func (s *Store) SetExcluded(ctx context.Context, plotID string, excluded bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plots SET excluded=? WHERE plot_id=?`,
		boolInt(excluded), plotID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("地块 %s 不存在", plotID)
	}
	return nil
}
