package judge_test

import (
	"math/rand/v2"
	"slices"
	"testing"

	"poptrio/server/internal/judge"
)

// ---- 随机源测试辅助 ----

// seqSource 依次返回给定颜色，用于固定补充序列的确定性测试；
// 次数不符（多索取即耗尽、少索取则残留）或颜色越界都会让测试失败。
func seqSource(t *testing.T, colors ...judge.Tile) judge.TileRNG {
	t.Helper()
	i := 0
	return func(n int) judge.Tile {
		if i >= len(colors) {
			t.Fatalf("random source exhausted at draw %d (scripted %d)", i+1, len(colors))
		}
		c := colors[i]
		i++
		if int(c) >= n {
			t.Fatalf("scripted color %d outside %d colors", c, n)
		}
		return c
	}
}

// pcgSource 返回以 seed 确定性的均匀随机源，用于统计性测试。
func pcgSource(seed uint64) judge.TileRNG {
	rng := rand.New(rand.NewPCG(seed, seed))
	return func(colors int) judge.Tile { return judge.Tile(rng.IntN(colors)) }
}

// column 返回 b 第 col 列自上而下的颜色序列。
func column(b judge.Board, col int) []judge.Tile {
	out := make([]judge.Tile, b.Config.Height)
	for r := range out {
		out[r] = b.At(judge.Pos{Col: col, Row: r})
	}
	return out
}

// ---- Settle：重力下落与顶部补充 ----

func TestSettleRefillsOnlyAtTop(t *testing.T) {
	b := painted9(t)
	// 清空最底行：所有存活棋子整体下落一格，空洞只出现在第 0 行。
	cells := make([]judge.Pos, 9)
	for c := range cells {
		cells[c] = judge.Pos{Col: c, Row: 8}
	}
	script := []judge.Tile{0, 1, 2, 3, 4, 5, 0, 1, 2}
	got := b.Settle(judge.Clear{Cells: cells}, seqSource(t, script...))
	for c := 0; c < 9; c++ {
		if top := got.At(judge.Pos{Col: c, Row: 0}); top != script[c] {
			t.Fatalf("col %d top tile = %d, want scripted %d", c, top, script[c])
		}
		for r := 1; r < 9; r++ {
			want := b.At(judge.Pos{Col: c, Row: r - 1})
			if gotAt := got.At(judge.Pos{Col: c, Row: r}); gotAt != want {
				t.Fatalf("col %d row %d = %d, want fallen tile %d", c, r, gotAt, want)
			}
		}
	}
}

func TestSettleCompactsColumnAndKeepsOrder(t *testing.T) {
	b := painted9(t)
	// 清除第 4 列中间两格：存活棋子保持相对顺序沉底，顶部两格按脚本补充。
	clear := judge.Clear{Cells: []judge.Pos{{Col: 4, Row: 4}, {Col: 4, Row: 5}}}
	got := b.Settle(clear, seqSource(t, 0, 5))
	wantCol := []judge.Tile{0, 5}
	for _, r := range []int{0, 1, 2, 3, 6, 7, 8} {
		wantCol = append(wantCol, b.At(judge.Pos{Col: 4, Row: r}))
	}
	if gotCol := column(got, 4); !slices.Equal(gotCol, wantCol) {
		t.Fatalf("col 4 after settle = %v, want %v", gotCol, wantCol)
	}
	for c := 0; c < 9; c++ {
		if c == 4 {
			continue
		}
		if gotCol := column(got, c); !slices.Equal(gotCol, column(b, c)) {
			t.Fatalf("col %d changed by settle: %v", c, gotCol)
		}
	}
}

func TestSettleWithoutClearIsIdentity(t *testing.T) {
	b := painted9(t)
	// 空脚本：只要 Settle 索取过一次颜色就会 panic，顺便断言零索取。
	got := b.Settle(judge.Clear{}, seqSource(t))
	if !slices.Equal(got.Tiles, b.Tiles) {
		t.Fatal("Settle with no clear changed the board")
	}
}

// ---- ApplySwap：多波 Cascade 结算 ----

// cascadeBoard 构造连锁测试棋盘：在 bg9 上把第 4 列第 5/7/8 行涂成 C。
// 交换 (4,5)↔(4,6) 后第 4 列第 6-8 行构成 C 连线，触发第一波消除。
func cascadeBoard(t *testing.T) judge.Board {
	t.Helper()
	return painted9(t, ov{4, 5, 'C'}, ov{4, 7, 'C'}, ov{4, 8, 'C'})
}

var cascadeSwap = judge.Swap{A: judge.Pos{Col: 4, Row: 5}, B: judge.Pos{Col: 4, Row: 6}}

// 第 4 列交换后清空第 6-8 行：存活棋子沉底为 E,F,A,B,C,E（第 3-8 行），
// 脚本 [D,E,F] 补充第 0-2 行后不再成线——单波收敛。
func TestApplySwapSingleWave(t *testing.T) {
	b := cascadeBoard(t)
	res, ok := judge.ApplySwap(b, cascadeSwap, "p1", seqSource(t, 3, 4, 5))
	if !ok {
		t.Fatal("ApplySwap = !ok, want a settled cascade")
	}
	if res.Operator != "p1" {
		t.Fatalf("Operator = %q, want %q", res.Operator, "p1")
	}
	if len(res.Waves) != 1 {
		t.Fatalf("got %d waves, want 1: %+v", len(res.Waves), res.Waves)
	}
	wantCells := []judge.Pos{{Col: 4, Row: 6}, {Col: 4, Row: 7}, {Col: 4, Row: 8}}
	if !slices.Equal(res.Waves[0].Clear.Cells, wantCells) {
		t.Fatalf("wave clear = %v, want %v", res.Waves[0].Clear.Cells, wantCells)
	}
	if res.Waves[0].Score != 3 || res.TotalScore != 3 {
		t.Fatalf("wave score = %d, total = %d, want 3/3", res.Waves[0].Score, res.TotalScore)
	}
	// 最终第 4 列：顶部补充 D,E,F + 沉底的 E,F,A,B,C,E。
	wantCol := []judge.Tile{3, 4, 5, 4, 5, 0, 1, 2, 4}
	if gotCol := column(res.Board, 4); !slices.Equal(gotCol, wantCol) {
		t.Fatalf("final col 4 = %v, want %v", gotCol, wantCol)
	}
	for c := 0; c < 9; c++ {
		if c == 4 {
			continue
		}
		if gotCol := column(res.Board, c); !slices.Equal(gotCol, column(b, c)) {
			t.Fatalf("final col %d changed unexpectedly: %v", c, gotCol)
		}
	}
	if _, lined := res.Board.FindClear(); lined {
		t.Fatal("final board still has lines after cascade settled")
	}
}

// 脚本 [A,A,A] 让第一波补充在第 4 列第 0-2 行构成 A 连线，触发第二波；
// 第二波补充 [D,E,F] 后收敛——两波全部归 p1 一人，总分 6。
func TestApplySwapSettlesAllCascadeWaves(t *testing.T) {
	b := cascadeBoard(t)
	res, ok := judge.ApplySwap(b, cascadeSwap, "p1", seqSource(t, 0, 0, 0, 3, 4, 5))
	if !ok {
		t.Fatal("ApplySwap = !ok, want a settled cascade")
	}
	if res.Operator != "p1" {
		t.Fatalf("Operator = %q, want %q", res.Operator, "p1")
	}
	if len(res.Waves) != 2 {
		t.Fatalf("got %d waves, want 2: %+v", len(res.Waves), res.Waves)
	}
	wantWaves := [][]judge.Pos{
		{{Col: 4, Row: 6}, {Col: 4, Row: 7}, {Col: 4, Row: 8}},
		{{Col: 4, Row: 0}, {Col: 4, Row: 1}, {Col: 4, Row: 2}},
	}
	total := 0
	for i, wave := range res.Waves {
		if !slices.Equal(wave.Clear.Cells, wantWaves[i]) {
			t.Fatalf("wave %d clear = %v, want %v", i, wave.Clear.Cells, wantWaves[i])
		}
		if wave.Score != len(wave.Clear.Cells) {
			t.Fatalf("wave %d score = %d, want %d", i, wave.Score, len(wave.Clear.Cells))
		}
		total += wave.Score
	}
	// 每波携带落定后棋盘：波与波首尾相接，最后一波即最终棋盘。
	if slices.Equal(res.Waves[0].Board.Tiles, res.Waves[1].Board.Tiles) ||
		!slices.Equal(res.Waves[1].Board.Tiles, res.Board.Tiles) {
		t.Fatal("per-wave boards do not chain wave 0 -> wave 1 -> final")
	}
	if res.TotalScore != total || res.TotalScore != 6 {
		t.Fatalf("TotalScore = %d, want sum of waves = 6", res.TotalScore)
	}
	wantCol := []judge.Tile{3, 4, 5, 4, 5, 0, 1, 2, 4}
	if gotCol := column(res.Board, 4); !slices.Equal(gotCol, wantCol) {
		t.Fatalf("final col 4 = %v, want %v", gotCol, wantCol)
	}
	if _, lined := res.Board.FindClear(); lined {
		t.Fatal("final board still has lines after cascade settled")
	}
}

func TestApplySwapBouncesWithoutLine(t *testing.T) {
	b := painted9(t) // bg9 图案无任何连线
	before := slices.Clone(b.Tiles)
	res, ok := judge.ApplySwap(b, judge.Swap{A: judge.Pos{Col: 4, Row: 4}, B: judge.Pos{Col: 5, Row: 4}}, "p1", seqSource(t))
	if ok {
		t.Fatalf("ApplySwap = ok with waves %+v, want bounce", res.Waves)
	}
	if res.Operator != "" || res.Waves != nil || res.TotalScore != 0 || res.Board.Tiles != nil {
		t.Fatalf("bounce result = %+v, want zero value", res)
	}
	if !slices.Equal(before, b.Tiles) {
		t.Fatal("bounced swap mutated the board")
	}
}

func TestApplySwapRejectsInvalidGeometry(t *testing.T) {
	b := painted9(t)
	res, ok := judge.ApplySwap(b, judge.Swap{A: judge.Pos{Col: 4, Row: 4}, B: judge.Pos{Col: 5, Row: 5}}, "p1", seqSource(t))
	if ok || res.Operator != "" || res.Waves != nil || res.TotalScore != 0 || res.Board.Tiles != nil {
		t.Fatalf("diagonal swap = (%+v, %v), want rejection", res, ok)
	}
}

func TestApplySwapDoesNotMutateInput(t *testing.T) {
	b := cascadeBoard(t)
	before := slices.Clone(b.Tiles)
	_, _ = judge.ApplySwap(b, cascadeSwap, "p1", seqSource(t, 0, 0, 0, 3, 4, 5))
	if !slices.Equal(before, b.Tiles) {
		t.Fatal("ApplySwap mutated the input board")
	}
}

// ---- Dead Board 检测 ----

func TestDeadBoardDetection(t *testing.T) {
	t.Run("bg9 pattern has no valid swap", func(t *testing.T) {
		b := painted9(t)
		if !b.IsDeadBoard() {
			t.Fatal("IsDeadBoard() = false, want true: no adjacent swap on the bg9 pattern forms a line")
		}
		if b.HasValidSwap() {
			t.Fatal("HasValidSwap() = true, want false")
		}
	})
	t.Run("diagonal mod-3 pattern is dead on 4x4", func(t *testing.T) {
		b := mustBoard(t, judge.Config{Width: 4, Height: 4, Colors: 3},
			"ABCA",
			"BCAB",
			"CABC",
			"ABCA",
		)
		if !b.IsDeadBoard() {
			t.Fatal("IsDeadBoard() = false, want true")
		}
	})
	t.Run("board with a line-forming swap is alive", func(t *testing.T) {
		// 把第 4 行第 4、6 列涂成 A：交换 (3,3)↔(4,3) 让 (4,3) 变 A，
		// 与已有的 (4,2)、(4,4) 构成横向 A 连线。
		b := painted9(t, ov{4, 4, 'A'}, ov{6, 4, 'A'})
		if b.IsDeadBoard() {
			t.Fatal("IsDeadBoard() = true, want false")
		}
		if !b.HasValidSwap() {
			t.Fatal("HasValidSwap() = false, want true")
		}
	})
	t.Run("matches brute-force oracle on random line-free boards", func(t *testing.T) {
		// 优化版 HasValidSwap 以「逐对交换副本 + FindClear」为定义 oracle：
		// 两者在无连线棋盘上必须完全一致（含等色交换、边缘、近连线等场景）。
		// 5x5 / 3 色让死局在线-free 样本中有足够出现率。
		cfg := judge.Config{Width: 5, Height: 5, Colors: 3}
		rand := pcgSource(2024)
		checked := 0
		for checked < 500 {
			tiles := make([]judge.Tile, cfg.Width*cfg.Height)
			for i := range tiles {
				tiles[i] = rand(cfg.Colors)
			}
			b, err := judge.NewBoard(cfg, tiles)
			if err != nil {
				t.Fatal(err)
			}
			if _, lined := b.FindClear(); lined {
				continue // oracle 仅对无连线棋盘与 HasValidSwap 等价
			}
			checked++
			if got, want := b.HasValidSwap(), bruteForceHasValidSwap(t, b); got != want {
				t.Fatalf("HasValidSwap() = %v, oracle says %v (board %v)", got, want, b.Tiles)
			}
		}
	})
}

// bruteForceHasValidSwap 逐对尝试全部相邻交换，任一交换副本出现连线即有效。
func bruteForceHasValidSwap(t *testing.T, b judge.Board) bool {
	t.Helper()
	for row := 0; row < b.Config.Height; row++ {
		for col := 0; col < b.Config.Width; col++ {
			p := judge.Pos{Col: col, Row: row}
			for _, q := range []judge.Pos{{Col: col + 1, Row: row}, {Col: col, Row: row + 1}} {
				s := judge.Swap{A: p, B: q}
				if !b.ValidSwap(s) {
					continue
				}
				tiles := slices.Clone(b.Tiles)
				i := p.Row*b.Config.Width + p.Col
				j := q.Row*b.Config.Width + q.Col
				tiles[i], tiles[j] = tiles[j], tiles[i]
				swapped, err := judge.NewBoard(b.Config, tiles)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := swapped.FindClear(); ok {
					return true
				}
			}
		}
	}
	return false
}
