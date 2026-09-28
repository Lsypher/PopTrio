package judge

import "fmt"

// maxGenAttempts 是 GenerateBoard 拒绝采样的尝试上限。
// 9x9 / 6 色下满足两约束的概率约百分之几，平均数十次即命中；上限只为
// 颜色数过少的退化配置兜底。
const maxGenAttempts = 10000

// GenerateBoard 生成一张满足两条约束的新棋盘：无任何现成连线，
// 且至少存在一步能成线的有效交换。初始棋盘与死局 Reshuffle 共用本函数
// （Reshuffle 的结果自动不同于触发重排的死局——死局无有效交换，必被拒绝）。
// 颜色数过少导致约束无法满足时返回错误。
func GenerateBoard(cfg Config, rand TileRNG) (Board, error) {
	if err := cfg.validate(); err != nil {
		return Board{}, err // 先于任何 rand 调用拒绝非法配置
	}
	tiles := make([]Tile, cfg.Width*cfg.Height)
	for attempt := 0; attempt < maxGenAttempts; attempt++ {
		for i := range tiles { // 按行优先逐格索取随机颜色
			tiles[i] = rand(cfg.Colors)
		}
		b, err := NewBoard(cfg, tiles)
		if err != nil {
			return Board{}, err // 已校验，不可达
		}
		if _, lined := b.FindClear(); lined {
			continue
		}
		if b.IsDeadBoard() {
			continue
		}
		return b, nil
	}
	return Board{}, fmt.Errorf("judge: no line-free board with a valid swap for %+v after %d attempts", cfg, maxGenAttempts)
}
