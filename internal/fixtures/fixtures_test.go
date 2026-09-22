package fixtures

import (
	"fmt"
	"testing"
)

func TestFixtureShape(t *testing.T) {
	plots, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(plots) != 54 {
		t.Fatalf("固定 fixture 应有 54 条记录（含 1 条重复坐标），得到 %d", len(plots))
	}
	cells := map[string]int{}
	yieldNA, heightNA := 0, 0
	treats := map[string]map[string]bool{}
	for _, p := range plots {
		cells[fmt.Sprintf("%d:%d", p.Row, p.Col)]++
		if p.YieldNA {
			yieldNA++
		}
		if p.HeightNA {
			heightNA++
		}
		if treats[p.Treatment] == nil {
			treats[p.Treatment] = map[string]bool{}
		}
		treats[p.Treatment][p.Block] = true
	}
	if cells["7:1"] != 2 {
		t.Fatalf("(7,1) 应为重复坐标，得到 %d 条", cells["7:1"])
	}
	if yieldNA != 1 || heightNA != 1 {
		t.Fatalf("期望产量/株高各 1 个缺区，得到 %d/%d", yieldNA, heightNA)
	}
	if len(treats["T4"]) != 1 {
		t.Fatalf("T4 必须只在单一区组，得到 %v", treats["T4"])
	}
	for _, tr := range []string{"T1", "T2", "T3"} {
		if len(treats[tr]) < 2 {
			t.Fatalf("%s 应跨至少两个区组", tr)
		}
	}
}
