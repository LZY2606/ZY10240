// Package fixtures embeds the fixed trial data used for replayable demos.
package fixtures

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"fieldbench/internal/domain"
)

//go:embed trial.csv
var TrialCSV string

// Load parses the fixed fixture CSV. Empty yield/height cells are kept as
// explicit missing observations, never converted to zero.
func Load() ([]domain.Plot, error) {
	return ParseCSV(TrialCSV)
}

// ParseCSV parses an uploaded fixture-compatible CSV stream.
func ParseCSV(data string) ([]domain.Plot, error) {
	r := csv.NewReader(strings.NewReader(data))
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("读取表头失败: %w", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	need := []string{"plot_id", "block", "row", "col", "line", "treatment"}
	for _, n := range need {
		if _, ok := idx[n]; !ok {
			return nil, fmt.Errorf("缺少必需列 %q", n)
		}
	}
	var plots []domain.Plot
	lineNo := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		lineNo++
		p := domain.Plot{
			ID: rec[idx["plot_id"]], Block: rec[idx["block"]],
			Line: rec[idx["line"]], Treatment: rec[idx["treatment"]],
		}
		p.Row, err = strconv.Atoi(rec[idx["row"]])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行行号非法: %w", lineNo, err)
		}
		p.Col, err = strconv.Atoi(rec[idx["col"]])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行列号非法: %w", lineNo, err)
		}
		if yi, ok := idx["yield"]; ok && yi < len(rec) && rec[yi] != "" {
			p.Yield, err = strconv.ParseFloat(rec[yi], 64)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行产量非法: %w", lineNo, err)
			}
		} else {
			p.YieldNA = true
		}
		if hi, ok := idx["height"]; ok && hi < len(rec) && rec[hi] != "" {
			p.Height, err = strconv.ParseFloat(rec[hi], 64)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行株高非法: %w", lineNo, err)
			}
		} else {
			p.HeightNA = true
		}
		plots = append(plots, p)
	}
	return plots, nil
}
