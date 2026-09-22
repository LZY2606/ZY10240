package store

import (
	"context"
	"testing"

	"fieldbench/internal/domain"
)

func TestRejectDuplicateUnitLastImportDoesNotWin(t *testing.T) {
	st, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	plots := []domain.Plot{
		{ID: "P1", Block: "B1", Row: 1, Col: 1, Line: "L1", Treatment: "T1", Yield: 5},
		{ID: "P1", Block: "B1", Row: 1, Col: 2, Line: "L9", Treatment: "T1", Yield: 9},
	}
	if err := st.ReplacePlots(ctx, plots); err == nil {
		t.Fatal("重复试验单元必须拒绝导入，而不是按最后导入覆盖")
	}
}

func TestCoordinateRevisionAndExclusion(t *testing.T) {
	st, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	plots := []domain.Plot{
		{ID: "P1", Block: "B1", Row: 1, Col: 1, Line: "L1", Treatment: "T1", Yield: 5},
		{ID: "P2", Block: "B1", Row: 1, Col: 2, Line: "L2", Treatment: "T2", Yield: 6},
	}
	if err := st.ReplacePlots(ctx, plots); err != nil {
		t.Fatal(err)
	}
	rev, err := st.UpdateCoordinates(ctx, "P2", 2, 2, "now")
	if err != nil {
		t.Fatal(err)
	}
	if rev.OldRow != 1 || rev.NewRow != 2 || rev.OldCol != 2 || rev.NewCol != 2 {
		t.Fatalf("修订记录错误: %+v", rev)
	}
	if err := st.SetExcluded(ctx, "P1", true); err != nil {
		t.Fatal(err)
	}
	got, _ := st.ListPlots(ctx)
	for _, p := range got {
		if p.ID == "P1" && !p.Excluded {
			t.Fatal("P1 应已被排除")
		}
	}
	revs, _ := st.ListRevisions(ctx)
	if len(revs) != 1 {
		t.Fatalf("应有 1 条修订，得到 %d", len(revs))
	}
}
