package judge_test

import (
	"reflect"
	"slices"
	"testing"

	"poptrio/server/internal/judge"
)

// ---- 测试辅助 ----

// mustBoard 由颜色字母行构造 Board，'A' = 颜色 0。rows[0] 为棋盘第 0 行（顶部）。
func mustBoard(t *testing.T, cfg judge.Config, rows ...string) judge.Board {
	t.Helper()
	if len(rows) != cfg.Height {
		t.Fatalf("got %d rows, want %d", len(rows), cfg.Height)
	}
	tiles := make([]judge.Tile, 0, cfg.Width*cfg.Height)
	for r, row := range rows {
		if len(row) != cfg.Width {
			t.Fatalf("row %d has %d cells, want %d", r, len(row), cfg.Width)
		}
		for c, ch := range row {
			color := int(ch - 'A')
			if color < 0 || color >= cfg.Colors {
				t.Fatalf("cell (col %d, row %d) letter %q outside %d colors", c, r, string(ch), cfg.Colors)
			}
			tiles = append(tiles, judge.Tile(color))
		}
	}
	b, err := judge.NewBoard(cfg, tiles)
	if err != nil {
		t.Fatalf("NewBoard: %v", err)
	}
	return b
}

// bg9 返回 9x9 背景图案：格子 (col, row) 的颜色为 (row+col)%6，
// 任意两个正交相邻格不同色，裸图案不含任何连线。
func bg9() []string {
	rows := make([]string, 9)
	for r := range rows {
		buf := make([]byte, 9)
		for c := range buf {
			buf[c] = byte('A' + (r+c)%6)
		}
		rows[r] = string(buf)
	}
	return rows
}

type ov struct {
	c, r  int
	color byte
}

// painted9 在 bg9 上叠加同色覆盖。任意两个正交相邻背景格不同色，因此背景格
// 至多把已放置的连续段向两端各延长一格；下方期望均按实际得到的棋盘书写。
func painted9(t *testing.T, ovs ...ov) judge.Board {
	t.Helper()
	rows := bg9()
	for _, o := range ovs {
		b := []byte(rows[o.r])
		b[o.c] = o.color
		rows[o.r] = string(b)
	}
	return mustBoard(t, judge.DefaultConfig(), rows...)
}

func posLess(a, b judge.Pos) int {
	if a.Row != b.Row {
		return a.Row - b.Row
	}
	return a.Col - b.Col
}

type lineWant struct {
	horiz     bool
	start     judge.Pos
	cellCount int
}

func actualLines(b judge.Board) []lineWant {
	var out []lineWant
	for _, l := range b.FindLines() {
		out = append(out, lineWant{
			horiz:     l.Cells[0].Row == l.Cells[1].Row,
			start:     l.Cells[0],
			cellCount: len(l.Cells),
		})
	}
	slices.SortFunc(out, func(a, b lineWant) int { return posLess(a.start, b.start) })
	return out
}

// ---- NewBoard ----

func TestNewBoardRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		cfg   judge.Config
		tiles []judge.Tile
	}{
		{"zero width", judge.Config{Width: 0, Height: 9, Colors: 6}, nil},
		{"zero height", judge.Config{Width: 9, Height: 0, Colors: 6}, nil},
		{"zero colors", judge.Config{Width: 9, Height: 9, Colors: 0}, nil},
		{"negative dimensions", judge.Config{Width: -1, Height: -1, Colors: -1}, nil},
		{"wrong tile count", judge.Config{Width: 2, Height: 2, Colors: 2}, []judge.Tile{0, 0, 0}},
		{"color id out of range", judge.Config{Width: 2, Height: 2, Colors: 2}, []judge.Tile{0, 1, 2, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := judge.NewBoard(tt.cfg, tt.tiles); err == nil {
				t.Fatalf("NewBoard(%+v, %v) = nil error, want error", tt.cfg, tt.tiles)
			}
		})
	}
}

func TestNewBoardCopiesTiles(t *testing.T) {
	tiles := []judge.Tile{0, 1, 1, 0}
	b, err := judge.NewBoard(judge.Config{Width: 2, Height: 2, Colors: 2}, tiles)
	if err != nil {
		t.Fatal(err)
	}
	tiles[0] = 1
	if b.At(judge.Pos{Col: 0, Row: 0}) != 0 {
		t.Fatal("board aliases the input tile slice")
	}
}

// ---- Swap 有效性 ----

func TestValidSwap(t *testing.T) {
	b := mustBoard(t, judge.DefaultConfig(), bg9()...)
	tests := []struct {
		name string
		a, b judge.Pos
		want bool
	}{
		{"adjacent right", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 5, Row: 4}, true},
		{"adjacent left", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 3, Row: 4}, true},
		{"adjacent up", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 4, Row: 3}, true},
		{"adjacent down", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 4, Row: 5}, true},
		{"corner cells adjacent", judge.Pos{Col: 0, Row: 0}, judge.Pos{Col: 0, Row: 1}, true},
		{"diagonal is not adjacent", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 5, Row: 5}, false},
		{"same cell", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 4, Row: 4}, false},
		{"two columns apart", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 6, Row: 4}, false},
		{"three rows apart", judge.Pos{Col: 4, Row: 4}, judge.Pos{Col: 4, Row: 7}, false},
		{"negative column out of bounds", judge.Pos{Col: 0, Row: 0}, judge.Pos{Col: -1, Row: 0}, false},
		{"negative row out of bounds", judge.Pos{Col: 0, Row: 0}, judge.Pos{Col: 0, Row: -1}, false},
		{"column past width out of bounds", judge.Pos{Col: 8, Row: 8}, judge.Pos{Col: 9, Row: 8}, false},
		{"row past height out of bounds", judge.Pos{Col: 8, Row: 8}, judge.Pos{Col: 8, Row: 9}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.ValidSwap(judge.Swap{A: tt.a, B: tt.b}); got != tt.want {
				t.Fatalf("ValidSwap(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---- 连线检测 ----

func TestFindLines(t *testing.T) {
	tests := []struct {
		name string
		ovs  []ov
		want []lineWant
	}{
		{
			// 第 2 行：第 3-5 列为 A
			name: "3 horizontal",
			ovs:  []ov{{3, 2, 'A'}, {4, 2, 'A'}, {5, 2, 'A'}},
			want: []lineWant{{true, judge.Pos{Col: 3, Row: 2}, 3}},
		},
		{
			// 第 4 行：第 2-5 列为 A
			name: "4 horizontal",
			ovs:  []ov{{2, 4, 'A'}, {3, 4, 'A'}, {4, 4, 'A'}, {5, 4, 'A'}},
			want: []lineWant{{true, judge.Pos{Col: 2, Row: 4}, 4}},
		},
		{
			// 第 4 行：第 2-6 列为 A
			name: "5 horizontal is one maximal line",
			ovs:  []ov{{2, 4, 'A'}, {3, 4, 'A'}, {4, 4, 'A'}, {5, 4, 'A'}, {6, 4, 'A'}},
			want: []lineWant{{true, judge.Pos{Col: 2, Row: 4}, 5}},
		},
		{
			// 第 4 行整行为 A
			name: "full row is one line of 9, not nested 3s",
			ovs:  []ov{{0, 4, 'A'}, {1, 4, 'A'}, {2, 4, 'A'}, {3, 4, 'A'}, {4, 4, 'A'}, {5, 4, 'A'}, {6, 4, 'A'}, {7, 4, 'A'}, {8, 4, 'A'}},
			want: []lineWant{{true, judge.Pos{Col: 0, Row: 4}, 9}},
		},
		{
			// 第 1 列：第 5-7 行为 A
			name: "3 vertical",
			ovs:  []ov{{1, 5, 'A'}, {1, 6, 'A'}, {1, 7, 'A'}},
			want: []lineWant{{false, judge.Pos{Col: 1, Row: 5}, 3}},
		},
		{
			// 第 7 列：第 0-3 行为 A
			name: "4 vertical",
			ovs:  []ov{{7, 0, 'A'}, {7, 1, 'A'}, {7, 2, 'A'}, {7, 3, 'A'}},
			want: []lineWant{{false, judge.Pos{Col: 7, Row: 0}, 4}},
		},
		{
			// 第 7 列：第 1-5 行为 A
			name: "5 vertical is one maximal line",
			ovs:  []ov{{7, 1, 'A'}, {7, 2, 'A'}, {7, 3, 'A'}, {7, 4, 'A'}, {7, 5, 'A'}},
			want: []lineWant{{false, judge.Pos{Col: 7, Row: 1}, 5}},
		},
		{
			name: "two cells in a row are not a line",
			ovs:  []ov{{4, 4, 'A'}, {5, 4, 'A'}},
			want: nil,
		},
		{
			name: "two cells in a column are not a line",
			ovs:  []ov{{4, 4, 'A'}, {4, 5, 'A'}},
			want: nil,
		},
		{
			name: "descending diagonal never clears",
			ovs:  []ov{{0, 0, 'A'}, {1, 1, 'A'}, {2, 2, 'A'}, {3, 3, 'A'}},
			want: nil,
		},
		{
			name: "ascending diagonal never clears",
			ovs:  []ov{{2, 0, 'A'}, {1, 1, 'A'}, {0, 2, 'A'}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := painted9(t, tt.ovs...)
			got := actualLines(b)
			want := append([]lineWant(nil), tt.want...)
			slices.SortFunc(want, func(a, b lineWant) int { return posLess(a.start, b.start) })
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("FindLines() = %+v, want %+v", got, want)
			}
		})
	}
}

// ---- Clear 合并与去重计分 ----

func TestFindClearMergesCrossingLines(t *testing.T) {
	tests := []struct {
		name      string
		ovs       []ov
		wantCells []judge.Pos
	}{
		{
			// 第 0 列第 2-4 行 + 第 4 行第 0-2 列
			name:      "L shape merges into one clear of 5",
			ovs:       []ov{{0, 2, 'A'}, {0, 3, 'A'}, {0, 4, 'A'}, {1, 4, 'A'}, {2, 4, 'A'}},
			wantCells: []judge.Pos{{Col: 0, Row: 2}, {Col: 0, Row: 3}, {Col: 0, Row: 4}, {Col: 1, Row: 4}, {Col: 2, Row: 4}},
		},
		{
			// 第 2 列第 0-2 行 + 第 2 行第 0-4 列
			name:      "T shape merges into one clear of 7",
			ovs:       []ov{{2, 0, 'A'}, {2, 1, 'A'}, {2, 2, 'A'}, {0, 2, 'A'}, {1, 2, 'A'}, {3, 2, 'A'}, {4, 2, 'A'}},
			wantCells: []judge.Pos{{Col: 2, Row: 0}, {Col: 2, Row: 1}, {Col: 0, Row: 2}, {Col: 1, Row: 2}, {Col: 2, Row: 2}, {Col: 3, Row: 2}, {Col: 4, Row: 2}},
		},
		{
			// 第 4 列第 1-3 行 + 第 2 行第 3-5 列
			name:      "cross merges into one clear of 5",
			ovs:       []ov{{4, 1, 'A'}, {3, 2, 'A'}, {4, 2, 'A'}, {5, 2, 'A'}, {4, 3, 'A'}},
			wantCells: []judge.Pos{{Col: 4, Row: 1}, {Col: 3, Row: 2}, {Col: 4, Row: 2}, {Col: 5, Row: 2}, {Col: 4, Row: 3}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := painted9(t, tt.ovs...)
			clear, ok := b.FindClear()
			if !ok {
				t.Fatal("FindClear() = !ok, want a clear")
			}
			if !slices.IsSortedFunc(clear.Cells, posLess) {
				t.Fatalf("clear cells not sorted row-major: %v", clear.Cells)
			}
			if !reflect.DeepEqual(clear.Cells, tt.wantCells) {
				t.Fatalf("FindClear() cells = %v, want %v", clear.Cells, tt.wantCells)
			}
			if clear.Score() != len(tt.wantCells) {
				t.Fatalf("Score() = %d, want %d (dedup: shared cells count once)", clear.Score(), len(tt.wantCells))
			}
		})
	}
}

func TestFindClearIndependentLines(t *testing.T) {
	// 第 0 行第 0-2 列（A）与第 5 行第 4-6 列（B）：两条互不相交的连线，合并为一次消除。
	b := painted9(t,
		ov{0, 0, 'A'}, ov{1, 0, 'A'}, ov{2, 0, 'A'},
		ov{4, 5, 'B'}, ov{5, 5, 'B'}, ov{6, 5, 'B'},
	)
	clear, ok := b.FindClear()
	if !ok {
		t.Fatal("FindClear() = !ok, want a clear")
	}
	want := []judge.Pos{
		{Col: 0, Row: 0}, {Col: 1, Row: 0}, {Col: 2, Row: 0},
		{Col: 4, Row: 5}, {Col: 5, Row: 5}, {Col: 6, Row: 5},
	}
	if !reflect.DeepEqual(clear.Cells, want) {
		t.Fatalf("FindClear() cells = %v, want %v", clear.Cells, want)
	}
	if clear.Score() != 6 {
		t.Fatalf("Score() = %d, want 6", clear.Score())
	}
}

func TestFindClearWithoutLines(t *testing.T) {
	b := painted9(t) // 纯背景，无覆盖
	if clear, ok := b.FindClear(); ok {
		t.Fatalf("FindClear() = %v with ok, want no clear", clear.Cells)
	}
}

// ---- 线性计分 ----

func TestLinearScore(t *testing.T) {
	tests := []struct {
		name  string
		cells int
		want  int
	}{
		{"three tiles score three", 3, 3},
		{"five tiles score five, no bonus", 5, 5},
		{"eight tiles score eight, no bonus", 8, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := make([]judge.Pos, tt.cells)
			for i := range cells {
				cells[i] = judge.Pos{Col: i % 9, Row: i / 9}
			}
			clear := judge.Clear{Cells: cells}
			if got := clear.Score(); got != tt.want {
				t.Fatalf("Score() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---- 配置注入 ----

func TestConfigIsInjected(t *testing.T) {
	t.Run("4x4 board with 3 colors", func(t *testing.T) {
		cfg := judge.Config{Width: 4, Height: 4, Colors: 3}
		b := mustBoard(t, cfg,
			"AAAB",
			"BCAC",
			"CBCA",
			"BACB",
		)
		lines := b.FindLines()
		if len(lines) != 1 || len(lines[0].Cells) != 3 || lines[0].Cells[0] != (judge.Pos{Col: 0, Row: 0}) {
			t.Fatalf("FindLines() = %+v, want one 3-line starting at (0,0)", lines)
		}
		clear, ok := b.FindClear()
		if !ok || clear.Score() != 3 {
			t.Fatalf("FindClear() = (%v, %v), want a 3-point clear", clear.Cells, ok)
		}
		if !b.ValidSwap(judge.Swap{A: judge.Pos{Col: 3, Row: 3}, B: judge.Pos{Col: 3, Row: 2}}) {
			t.Error("adjacent swap on 4x4 board rejected")
		}
		if b.ValidSwap(judge.Swap{A: judge.Pos{Col: 3, Row: 3}, B: judge.Pos{Col: 4, Row: 3}}) {
			t.Error("out-of-bounds swap accepted on 4x4 board")
		}
	})
	t.Run("non-square 5x3 board", func(t *testing.T) {
		cfg := judge.Config{Width: 5, Height: 3, Colors: 4}
		b := mustBoard(t, cfg,
			"ABCAD",
			"BADAD",
			"CDABD",
		)
		lines := b.FindLines()
		if len(lines) != 1 || len(lines[0].Cells) != 3 || lines[0].Cells[0] != (judge.Pos{Col: 4, Row: 0}) {
			t.Fatalf("FindLines() = %+v, want one 3-line starting at (4,0)", lines)
		}
	})
	t.Run("DefaultConfig is 9x9 with 6 colors", func(t *testing.T) {
		want := judge.Config{Width: 9, Height: 9, Colors: 6}
		if judge.DefaultConfig() != want {
			t.Fatalf("DefaultConfig() = %+v, want %+v", judge.DefaultConfig(), want)
		}
	})
}

// ---- 纯度 ----

func TestJudgeFunctionsArePure(t *testing.T) {
	b := painted9(t,
		ov{0, 2, 'A'}, ov{0, 3, 'A'}, ov{0, 4, 'A'},
		ov{1, 4, 'A'}, ov{2, 4, 'A'},
	)
	before := slices.Clone(b.Tiles)

	b.ValidSwap(judge.Swap{A: judge.Pos{Col: 5, Row: 5}, B: judge.Pos{Col: 6, Row: 5}})
	_ = b.FindLines()
	_, _ = b.FindClear()

	if !slices.Equal(before, b.Tiles) {
		t.Fatal("judge functions mutated the board")
	}

	clear1, _ := b.FindClear()
	clear2, _ := b.FindClear()
	if !reflect.DeepEqual(clear1, clear2) {
		t.Fatal("FindClear is not deterministic")
	}
}
