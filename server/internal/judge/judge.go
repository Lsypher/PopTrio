// Package judge 以纯函数实现 PopTrio 的三消规则判定。
//
// 无 IO、无副作用、无全局状态：棋盘尺寸与颜色数经 Config 注入。房间 Actor
// 是唯一调用方并持有全部棋盘状态；judge 只检视或推导传入的棋盘。
package judge

import (
	"fmt"
	"sort"
)

// Config 是注入的棋盘配置。
type Config struct {
	Width  int // 列数，> 0
	Height int // 行数，> 0
	Colors int // 棋子颜色数，> 0
}

// DefaultConfig 是 MVP 配置：9x9 棋盘、6 种棋子颜色。
func DefaultConfig() Config { return Config{Width: 9, Height: 9, Colors: 6} }

// Tile 是 [0, Config.Colors) 范围内的颜色 ID。
type Tile uint8

// Pos 是棋盘上的一个格子。原点在左上角：Col ∈ [0, Width)，Row ∈ [0, Height)。
type Pos struct {
	Col int
	Row int
}

// Board 是填满棋子的 Width x Height 网格，按行优先存储。
type Board struct {
	Config Config
	Tiles  []Tile // 长度 == Width*Height；下标 = Row*Width + Col
}

// validate 校验配置合法性。
func (cfg Config) validate() error {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Colors <= 0 {
		return fmt.Errorf("judge: invalid config %+v", cfg)
	}
	return nil
}

// NewBoard 校验 cfg 与 tiles，返回持有 tiles 副本的 Board。
func NewBoard(cfg Config, tiles []Tile) (Board, error) {
	if err := cfg.validate(); err != nil {
		return Board{}, err
	}
	if len(tiles) != cfg.Width*cfg.Height {
		return Board{}, fmt.Errorf("judge: got %d tiles, want %d for %dx%d board",
			len(tiles), cfg.Width*cfg.Height, cfg.Width, cfg.Height)
	}
	for i, t := range tiles {
		if int(t) >= cfg.Colors {
			return Board{}, fmt.Errorf("judge: tile %d has color %d, want < %d", i, t, cfg.Colors)
		}
	}
	owned := make([]Tile, len(tiles))
	copy(owned, tiles)
	return Board{Config: cfg, Tiles: owned}, nil
}

// At 返回 p 处的棋子。越界位置会 panic。
func (b Board) At(p Pos) Tile {
	return b.Tiles[p.Row*b.Config.Width+p.Col]
}

// Swap 是玩家交换 A、B 两格棋子的请求。
type Swap struct {
	A, B Pos
}

// ValidSwap 报告 s 是否为合法的交换请求：两格均在界内、互不相同且正交相邻。
// 交换是否真正成线是另一个问题：把交换应用到棋盘后调用 FindClear。
func (b Board) ValidSwap(s Swap) bool {
	if !b.contains(s.A) || !b.contains(s.B) {
		return false
	}
	dCol := abs(s.A.Col - s.B.Col)
	dRow := abs(s.A.Row - s.B.Row)
	return dCol+dRow == 1
}

// HasValidSwap 报告棋盘上是否存在至少一个交换后能形成连线的相邻交换。
// 假定棋盘无现成连线（对局流程保证：棋盘只在 Cascade 结算完毕后接受交换）。
func (b Board) HasValidSwap() bool {
	for row := 0; row < b.Config.Height; row++ {
		for col := 0; col < b.Config.Width; col++ {
			p := Pos{Col: col, Row: row}
			if col+1 < b.Config.Width && b.swapFormsLine(p, Pos{Col: col + 1, Row: row}) {
				return true
			}
			if row+1 < b.Config.Height && b.swapFormsLine(p, Pos{Col: col, Row: row + 1}) {
				return true
			}
		}
	}
	return false
}

// IsDeadBoard 报告棋盘是否为死局：不存在任何有效交换。
func (b Board) IsDeadBoard() bool { return !b.HasValidSwap() }

// swapFormsLine 报告交换 p、q 两格后，p 或 q 是否处于 ≥3 连线中。
// 交换只影响这两格，其余格子无需检视；等色交换不改变棋盘，恒为 false。
func (b Board) swapFormsLine(p, q Pos) bool {
	a, c := b.At(p), b.At(q)
	if a == c {
		return false
	}
	return b.runThrough(p, c) || b.runThrough(q, a)
}

// runThrough 报告若 p 处棋子为 color，p 是否处于横向或纵向 ≥3 连线中。
func (b Board) runThrough(p Pos, color Tile) bool {
	return b.extend(p, color, 1, 0) >= 3 || b.extend(p, color, 0, 1) >= 3
}

// extend 统计若 p 处棋子为 color，含 p 沿 (dCol, dRow) 两端的最大连续数。
func (b Board) extend(p Pos, color Tile, dCol, dRow int) int {
	n := 1
	for next := (Pos{Col: p.Col + dCol, Row: p.Row + dRow}); b.contains(next) && b.At(next) == color; next = (Pos{Col: next.Col + dCol, Row: next.Row + dRow}) {
		n++
	}
	for next := (Pos{Col: p.Col - dCol, Row: p.Row - dRow}); b.contains(next) && b.At(next) == color; next = (Pos{Col: next.Col - dCol, Row: next.Row - dRow}) {
		n++
	}
	return n
}

func (b Board) contains(p Pos) bool {
	return p.Col >= 0 && p.Col < b.Config.Width && p.Row >= 0 && p.Row < b.Config.Height
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Line 是 ≥3 枚同色棋子的横向或纵向最大连续段。Cells 沿连线方向有序。
type Line struct {
	Cells []Pos
}

// Clear 是一次消除：去重后待移除的棋子集合，按行优先排序。
// 交叉连线（L/T 形）合并为一次 Clear。
type Clear struct {
	Cells []Pos
}

// Score 返回该次消除的得分：每枚棋子恰好 1 分——线性，任何场景无加成。
func (c Clear) Score() int { return len(c.Cells) }

// FindLines 返回棋盘上所有 ≥3 枚同色的横向或纵向最大连续段，
// 先自上而下逐行、再自左向右逐列扫描。斜向排列永不构成连线。
func (b Board) FindLines() []Line {
	var lines []Line
	for row := 0; row < b.Config.Height; row++ {
		for col := 0; col < b.Config.Width; {
			n := b.runLen(Pos{col, row}, 1, 0)
			if n >= 3 {
				lines = append(lines, newLine(Pos{col, row}, 1, 0, n))
			}
			col += n
		}
	}
	for col := 0; col < b.Config.Width; col++ {
		for row := 0; row < b.Config.Height; {
			n := b.runLen(Pos{col, row}, 0, 1)
			if n >= 3 {
				lines = append(lines, newLine(Pos{col, row}, 0, 1, n))
			}
			row += n
		}
	}
	return lines
}

// FindClear 把棋盘上的所有 Line 合并为一次 Clear，对共享格去重，
// 使 L/T 交叉格恰好计一次。棋盘无连线时 ok 为 false。
func (b Board) FindClear() (Clear, bool) {
	var cells []Pos
	seen := make(map[Pos]struct{})
	for _, line := range b.FindLines() {
		for _, p := range line.Cells {
			if _, dup := seen[p]; !dup {
				seen[p] = struct{}{}
				cells = append(cells, p)
			}
		}
	}
	if len(cells) == 0 {
		return Clear{}, false
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].Row != cells[j].Row {
			return cells[i].Row < cells[j].Row
		}
		return cells[i].Col < cells[j].Col
	})
	return Clear{Cells: cells}, true
}

// runLen 从 p 出发沿 (dCol, dRow) 步进统计同色棋子数，
// 直到下一格越界或颜色不同为止。
func (b Board) runLen(p Pos, dCol, dRow int) int {
	color := b.At(p)
	n := 1
	for {
		next := Pos{Col: p.Col + dCol*n, Row: p.Row + dRow*n}
		if !b.contains(next) || b.At(next) != color {
			return n
		}
		n++
	}
}

func newLine(start Pos, dCol, dRow, n int) Line {
	cells := make([]Pos, n)
	for i := range cells {
		cells[i] = Pos{Col: start.Col + dCol*i, Row: start.Row + dRow*i}
	}
	return Line{Cells: cells}
}
