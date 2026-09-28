package judge

import (
	"fmt"
	"slices"
)

// TileRNG 逐个产出随机棋子颜色：入参为颜色上界 colors，返回值必须落在
// [0, colors)。判定器除该函数外无任何随机性；纯度以
// "同一棋盘 + 同一随机序列 → 同一结果" 为准。
type TileRNG func(colors int) Tile

// Wave 是连锁中的一波结算：一次消除及其得分与落定后的棋盘。
type Wave struct {
	Clear Clear // 本波消除的棋子（去重、行优先）
	Score int   // 本波得分，等于 len(Clear.Cells)
	Board Board // 本波消除并结算（下落 + 补充）后的棋盘；最后一波即 CascadeResult.Board
}

// CascadeResult 是一次 Swap 触发的全部连锁波次的结算结果。
type CascadeResult struct {
	Operator   string // 得分归属的操作者，原样回显入参
	Waves      []Wave // 按结算顺序排列的全部波次
	TotalScore int    // 所有波次得分之和，归 Operator 一人
	Board      Board  // 全部波次结算后的最终棋盘
}

// Settle 对移除 c 中棋子后的棋盘施加重力并从顶部随机补充：
// 每列存活棋子保持原有相对顺序沉底，空洞全部位于列顶，按列自左向右、
// 列内自上而下逐格向 rand 索取新颜色。c 为空时不索取颜色，返回等值副本。
func (b Board) Settle(c Clear, rand TileRNG) Board {
	removed := make(map[Pos]struct{}, len(c.Cells))
	for _, p := range c.Cells {
		_ = b.At(p) // 越界与 At 一致，直接 panic
		removed[p] = struct{}{}
	}
	w, h := b.Config.Width, b.Config.Height
	tiles := make([]Tile, len(b.Tiles))
	for col := 0; col < w; col++ {
		write := h - 1
		for row := h - 1; row >= 0; row-- {
			if _, gone := removed[Pos{Col: col, Row: row}]; gone {
				continue
			}
			tiles[write*w+col] = b.Tiles[row*w+col]
			write--
		}
		for row := 0; row <= write; row++ {
			tiles[row*w+col] = rand(b.Config.Colors)
		}
	}
	board, err := NewBoard(b.Config, tiles)
	if err != nil {
		panic(fmt.Sprintf("judge: Settle produced an invalid board: %v", err)) // 尺寸与颜色域不变，不可达
	}
	return board
}

// ApplySwap 校验并应用一次玩家交换，随后把全部连锁波次一次性结算完毕：
// 只要棋盘仍有连线就继续消除、下落、补充，直至无连线，永不把半结算
// 状态留给调用方。所有波次的得分归 operator 一人（Attribution）。
// 假定输入棋盘无现成连线（对局流程保证）；否则交换前已存在的连线
// 也会作为第一波被结算并计入 operator。
//
// 返回 ok=false 表示该交换不产生任何消除——几何非法或交换后无连线
// （回弹）——此时结果为零值，原棋盘不受影响。调用方如需区分两者，
// 可先用 ValidSwap 检查几何合法性。
func ApplySwap(b Board, s Swap, operator string, rand TileRNG) (CascadeResult, bool) {
	if !b.ValidSwap(s) {
		return CascadeResult{}, false
	}
	var waves []Wave
	total := 0
	cur := b.swapped(s)
	for {
		clear, ok := cur.FindClear()
		if !ok {
			break
		}
		cur = cur.Settle(clear, rand)
		waves = append(waves, Wave{Clear: clear, Score: clear.Score(), Board: cur})
		total += clear.Score()
	}
	if len(waves) == 0 {
		return CascadeResult{}, false // 无效交换：回弹，棋盘不变
	}
	return CascadeResult{Operator: operator, Waves: waves, TotalScore: total, Board: cur}, true
}

// swapped 返回交换 s 两格棋子后的棋盘副本。
func (b Board) swapped(s Swap) Board {
	tiles := slices.Clone(b.Tiles)
	i := s.A.Row*b.Config.Width + s.A.Col
	j := s.B.Row*b.Config.Width + s.B.Col
	tiles[i], tiles[j] = tiles[j], tiles[i]
	return Board{Config: b.Config, Tiles: tiles}
}
