package room_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"poptrio/server/internal/judge"
	"poptrio/server/internal/room"
)

// ---- 测试辅助 ----

// baseTime 是固定服务端时钟：所有快照截止时间戳均确定性可断言。
var baseTime = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// scriptedRNG 依次返回给定颜色。房间在事件循环 goroutine 中调用本函数，
// 故脚本耗尽 / 颜色越界用 panic 报错（t.Fatalf 不可跨 goroutine）。
func scriptedRNG(t *testing.T, tiles ...judge.Tile) judge.TileRNG {
	t.Helper()
	i := 0
	return func(colors int) judge.Tile {
		if i >= len(tiles) {
			panic(fmt.Sprintf("scripted RNG exhausted at draw %d (scripted %d)", i+1, len(tiles)))
		}
		c := tiles[i]
		i++
		if int(c) >= colors {
			panic(fmt.Sprintf("scripted color %d outside %d colors", c, colors))
		}
		return c
	}
}

// rowsToTiles 把颜色字母行转为 Tile 切片（'A' = 颜色 0）。
func rowsToTiles(t *testing.T, rows []string) []judge.Tile {
	t.Helper()
	tiles := make([]judge.Tile, 0, len(rows)*len(rows[0]))
	for r, row := range rows {
		if len(row) != 9 {
			t.Fatalf("row %d has %d cells, want 9", r, len(row))
		}
		for c, ch := range row {
			n := int(ch - 'A')
			if n < 0 || n >= 6 {
				t.Fatalf("cell (col %d, row %d) letter %q outside 6 colors", c, r, string(ch))
			}
			tiles = append(tiles, judge.Tile(n))
		}
	}
	return tiles
}

// bgRows 返回 9x9 背景图案：(row+col)%6，任意正交相邻格异色。
func bgRows() []string {
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

// board1Rows 是开局脚本棋盘：背景图案 + 第 0 行改写为 B B A B E F A B C。
// 交换 (0,2)<->(0,3) 恰好一波消除第 0 行前三格（得 3 分）；结算补充
// D E F 后整盘成为死局（触发 Reshuffle）；重排复用 board1（有可行交换）。
var board1Rows = func() []string {
	rows := bgRows()
	rows[0] = "BBABEFABC"
	return rows
}()

// board1Tiles 返回开局脚本棋盘的 81 个颜色（喂给 GenerateBoard 的脚本）。
func board1Tiles(t *testing.T) []judge.Tile {
	t.Helper()
	return rowsToTiles(t, board1Rows)
}

// fullScript = 开局棋盘(81) + 死局补充(3) + 重排棋盘(81)。
func fullScript(t *testing.T) []judge.Tile {
	t.Helper()
	tiles := board1Tiles(t)
	tiles = append(tiles, 3, 4, 5)
	tiles = append(tiles, board1Tiles(t)...)
	return tiles
}

// swapA 是脚本棋盘上的成线交换：(0,2)<->(0,3)，A 与 B 互换成 B 三连。
var swapA = judge.Swap{A: judge.Pos{Col: 2, Row: 0}, B: judge.Pos{Col: 3, Row: 0}}

// testDeps 组装确定性依赖：脚本随机源、指定先手、固定时钟。
func testDeps(t *testing.T, coin int, script []judge.Tile) room.Deps {
	t.Helper()
	return room.Deps{
		TileRNG:   scriptedRNG(t, script...),
		FirstCoin: func() int { return coin },
		Now:       func() time.Time { return baseTime },
	}
}

// runRoom 启动房间事件循环，执行 drive 注入合成命令，等待终局。
// 断言双座位收到逐帧一致的广播流，返回该流。
func runRoom(t *testing.T, cfg room.Config, deps room.Deps, drive func(*room.Room)) []room.Frame {
	t.Helper()
	var left, right []room.Frame
	r := room.New([2]string{"P0", "P1"}, cfg, deps, [2]room.Sink{
		func(f room.Frame) { left = append(left, f) },
		func(f room.Frame) { right = append(right, f) },
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run()
	}()
	drive(r)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("room did not settle within timeout")
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("two seats received different streams: %d vs %d frames", len(left), len(right))
	}
	return left
}

func kinds(frames []room.Frame) []room.FrameKind {
	out := make([]room.FrameKind, len(frames))
	for i, f := range frames {
		out[i] = f.Kind()
	}
	return out
}

// snapshotOf 提取快照帧内嵌的快照（MatchStarted / TurnStarted）。
func snapshotOf(t *testing.T, f room.Frame) room.Snapshot {
	t.Helper()
	switch v := f.(type) {
	case room.MatchStartedFrame:
		return v.Snapshot
	case room.TurnStartedFrame:
		return v.Snapshot
	default:
		t.Fatalf("frame %s does not carry a snapshot", f.Kind())
		return room.Snapshot{}
	}
}

// closeAllTurns 依次投递全部回合的窗口截止命令，驱动对局走向结算。
func closeAllTurns(r *room.Room, totalTurns int) {
	for turn := 1; turn <= totalTurns; turn++ {
		r.SubmitDeadline(turn)
	}
}

// expectedDeadBoard 独立重算 swapA 触发一波消除 + D E F 补充后的死局棋盘。
func expectedDeadBoard(t *testing.T) []judge.Tile {
	t.Helper()
	b, err := judge.NewBoard(judge.DefaultConfig(), board1Tiles(t))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := judge.ApplySwap(b, swapA, "P", scriptedRNG(t, 3, 4, 5))
	if !ok {
		t.Fatal("fixture broken: swapA cleared nothing")
	}
	return res.Board.Tiles
}

// ---- 棋盘脚本守护 ----

// TestFixtureBoard1Script 守护测试脚本：开局棋盘满足生成两约束；
// swapA 恰好一波消 3 格得 3 分；结算后整盘死局（Reshuffle 路径可达）。
func TestFixtureBoard1Script(t *testing.T) {
	b, err := judge.NewBoard(judge.DefaultConfig(), board1Tiles(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, lined := b.FindClear(); lined {
		t.Fatal("fixture board has pre-existing lines")
	}
	if b.IsDeadBoard() {
		t.Fatal("fixture board has no valid swap")
	}
	res, ok := judge.ApplySwap(b, swapA, "P", scriptedRNG(t, 3, 4, 5))
	if !ok {
		t.Fatal("fixture broken: swapA cleared nothing")
	}
	if len(res.Waves) != 1 || res.TotalScore != 3 {
		t.Fatalf("fixture swap got %d waves / %d points, want 1 wave / 3 points", len(res.Waves), res.TotalScore)
	}
	if !res.Board.IsDeadBoard() {
		t.Fatal("fixture post-settle board should be a dead board")
	}
}

// ---- 完整一局：事件帧序列 ----

// TestFullGameFrameSequence 合成命令打完整局：帧序 = 对局开始 + 结果帧 +
// 死局重排 + 11 个回合快照 + 结算；各帧内容符合协议语义（ADR-0003）。
func TestFullGameFrameSequence(t *testing.T) {
	frames := runRoom(t, room.DefaultConfig(), testDeps(t, 0, fullScript(t)), func(r *room.Room) {
		r.SubmitSwap(0, swapA)
		closeAllTurns(r, 12)
	})

	want := []room.FrameKind{room.KindMatchStarted, room.KindSwapResult, room.KindReshuffle}
	for i := 0; i < 11; i++ {
		want = append(want, room.KindTurnStarted)
	}
	want = append(want, room.KindSettlement)
	if !slices.Equal(kinds(frames), want) {
		t.Fatalf("frame kinds = %v, want %v", kinds(frames), want)
	}

	// MatchStarted：先手 + 回合 1 初始快照。
	start, ok := frames[0].(room.MatchStartedFrame)
	if !ok {
		t.Fatalf("first frame is %s, want match_started", frames[0].Kind())
	}
	if start.Seats != [2]string{"P0", "P1"} || start.FirstSeat != 0 {
		t.Fatalf("match started = %+v, want seats [P0 P1] first 0", start)
	}
	wantDeadline := baseTime.Add(10 * time.Second)
	snap := start.Snapshot
	if snap.Turn != 1 || snap.Operator != 0 || snap.Scores != [2]int{0, 0} {
		t.Fatalf("initial snapshot = %+v, want turn 1 operator 0 scores [0 0]", snap)
	}
	if !snap.Deadline.Equal(wantDeadline) {
		t.Fatalf("initial deadline = %v, want %v", snap.Deadline, wantDeadline)
	}
	if !slices.Equal(snap.Board, board1Tiles(t)) {
		t.Fatal("initial board does not match scripted board1")
	}

	// SwapResult：一波 3 格得 3 分，全归操作者座位 0。
	res := frames[1].(room.SwapResultFrame)
	if res.Operator != 0 || len(res.Waves) != 1 || res.Waves[0].Score != 3 {
		t.Fatalf("swap result = operator %d, %d waves, want operator 0 / 1 wave scoring 3", res.Operator, len(res.Waves))
	}
	wantClear := []judge.Pos{{Col: 0, Row: 0}, {Col: 1, Row: 0}, {Col: 2, Row: 0}}
	if !slices.Equal(res.Waves[0].Clear, wantClear) {
		t.Fatalf("clear cells = %v, want %v", res.Waves[0].Clear, wantClear)
	}
	if !slices.Equal(res.Waves[0].Board, expectedDeadBoard(t)) {
		t.Fatal("wave board does not match judge-recomputed dead board")
	}
	if res.Scores != [2]int{3, 0} {
		t.Fatalf("scores after swap = %v, want [3 0]", res.Scores)
	}
	if !slices.Equal(res.Board, expectedDeadBoard(t)) {
		t.Fatal("board after swap does not match judge-recomputed dead board")
	}

	// Reshuffle：棋盘更新，分数与回合不变。
	shuffle := frames[2].(room.ReshuffleFrame)
	if !slices.Equal(shuffle.Board, board1Tiles(t)) {
		t.Fatal("reshuffled board does not match scripted board1")
	}
	if shuffle.Scores != [2]int{3, 0} || shuffle.Turn != 1 {
		t.Fatalf("reshuffle = %+v, want scores [3 0] turn 1 unchanged", shuffle)
	}

	// TurnStarted：回合交替流转，操作方随回合交替。
	for i, f := range frames[3 : len(frames)-1] {
		snap := snapshotOf(t, f)
		turn := i + 2
		wantOp := (turn - 1) % 2 // 先手 0：回合 n 的操作方 = (n-1)%2
		if snap.Turn != turn || snap.Operator != wantOp || snap.Scores != [2]int{3, 0} {
			t.Fatalf("turn snapshot %d = %+v, want turn %d operator %d scores [3 0]", turn, snap, turn, wantOp)
		}
		if !snap.Deadline.Equal(wantDeadline) {
			t.Fatalf("turn %d deadline = %v, want %v", turn, snap.Deadline, wantDeadline)
		}
		if !slices.Equal(snap.Board, board1Tiles(t)) {
			t.Fatalf("turn %d board is not the reshuffled board", turn)
		}
	}

	// Settlement：3:0，座位 0 胜，位于流末尾。
	settle := frames[len(frames)-1].(room.SettlementFrame)
	if settle.Draw || settle.Winner != 0 || settle.Scores != [2]int{3, 0} {
		t.Fatalf("settlement = %+v, want win for seat 0 with [3 0]", settle)
	}
}

// ---- 结算三路径 ----

// TestSettlementPaths 覆盖胜/负/Draw 三条终局路径。
func TestSettlementPaths(t *testing.T) {
	tests := []struct {
		name      string
		coin      int
		swapper   int // 执行 swapA 的座位；-1 = 无人交换
		wantDraw  bool
		wantWin   int
		wantScore [2]int
	}{
		{"draw when nobody scores", 0, -1, true, -1, [2]int{0, 0}},
		{"first seat wins", 0, 0, false, 0, [2]int{3, 0}},
		{"second seat wins", 1, 1, false, 1, [2]int{0, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames := runRoom(t, room.DefaultConfig(), testDeps(t, tt.coin, fullScript(t)), func(r *room.Room) {
				if tt.swapper >= 0 {
					r.SubmitSwap(tt.swapper, swapA)
				}
				closeAllTurns(r, 12)
			})
			start := frames[0].(room.MatchStartedFrame)
			if start.FirstSeat != tt.coin {
				t.Fatalf("first seat = %d, want coin %d", start.FirstSeat, tt.coin)
			}
			settle, ok := frames[len(frames)-1].(room.SettlementFrame)
			if !ok {
				t.Fatalf("last frame is %s, want settlement", frames[len(frames)-1].Kind())
			}
			if settle.Draw != tt.wantDraw || settle.Winner != tt.wantWin || settle.Scores != tt.wantScore {
				t.Fatalf("settlement = %+v, want draw %v winner %d scores %v",
					settle, tt.wantDraw, tt.wantWin, tt.wantScore)
			}
		})
	}
}

// ---- 无效交换：回弹不改变状态、不结束回合 ----

// TestInvalidSwapsBounce 无效 Swap（越权 / 几何非法 / 不成线）按到达顺序
// 逐一拒绝，棋盘不变、回合不流转，最终照常打满 12 回合并 Draw。
func TestInvalidSwapsBounce(t *testing.T) {
	nonAdjacent := judge.Swap{A: judge.Pos{Col: 0, Row: 0}, B: judge.Pos{Col: 5, Row: 0}}
	sameColor := judge.Swap{A: judge.Pos{Col: 0, Row: 0}, B: judge.Pos{Col: 1, Row: 0}} // 均为 B

	frames := runRoom(t, room.DefaultConfig(), testDeps(t, 0, board1Tiles(t)), func(r *room.Room) {
		r.SubmitSwap(1, swapA)       // 回合 1 操作方是座位 0
		r.SubmitSwap(0, nonAdjacent) // 不相邻
		r.SubmitSwap(0, sameColor)   // 相邻但不成线
		closeAllTurns(r, 12)
	})

	want := []room.FrameKind{room.KindMatchStarted,
		room.KindSwapRejected, room.KindSwapRejected, room.KindSwapRejected}
	for i := 0; i < 11; i++ {
		want = append(want, room.KindTurnStarted)
	}
	want = append(want, room.KindSettlement)
	if !slices.Equal(kinds(frames), want) {
		t.Fatalf("frame kinds = %v, want rejections followed by normal flow", kinds(frames))
	}

	wantReasons := []room.RejectReason{room.RejectNotOperator, room.RejectInvalidSwap, room.RejectNoClear}
	for i, reason := range wantReasons {
		rej := frames[i+1].(room.SwapRejectedFrame)
		if rej.Reason != reason {
			t.Fatalf("rejection %d reason = %s, want %s", i, rej.Reason, reason)
		}
	}

	// 回合 2 快照棋盘与初始棋盘一致：无效交换未改变棋盘。
	snap := snapshotOf(t, frames[4])
	if !slices.Equal(snap.Board, board1Tiles(t)) {
		t.Fatal("board changed after rejected swaps")
	}

	settle := frames[len(frames)-1].(room.SettlementFrame)
	if !settle.Draw || settle.Scores != [2]int{0, 0} {
		t.Fatalf("settlement = %+v, want draw [0 0]", settle)
	}
}

// ---- 窗口截止与 Swap 的到达顺序竞态 ----

// TestDeadlineFirstRejectsLateSwap 截止命令先行入队：Swap 按新回合归属
// 判定，操作方已易手必拒，棋盘不变——结果确定，无竞态（ADR-0004）。
func TestDeadlineFirstRejectsLateSwap(t *testing.T) {
	frames := runRoom(t, room.DefaultConfig(), testDeps(t, 0, board1Tiles(t)), func(r *room.Room) {
		r.SubmitDeadline(1) // 先于 Swap 到达：关闭回合 1
		r.SubmitSwap(0, swapA)
		closeAllTurns(r, 12) // 1 已消费，2..12 正常
	})

	turn2 := -1
	rejected := -1
	for i, f := range frames {
		switch v := f.(type) {
		case room.TurnStartedFrame:
			if v.Turn == 2 && turn2 == -1 {
				turn2 = i
			}
		case room.SwapRejectedFrame:
			if rejected == -1 {
				rejected = i
				if v.Reason != room.RejectNotOperator {
					t.Fatalf("late swap reason = %s, want not_operator", v.Reason)
				}
				if v.Seat != 0 {
					t.Fatalf("late swap seat = %d, want 0", v.Seat)
				}
			}
		}
	}
	if turn2 == -1 || rejected == -1 || turn2 > rejected {
		t.Fatalf("expected turn_started(2) before swap_rejected, got turn2@%d rejected@%d", turn2, rejected)
	}
	// 拒绝前的回合 2 快照棋盘 = 初始棋盘：迟到交换未生效。
	snap := snapshotOf(t, frames[turn2])
	if !slices.Equal(snap.Board, board1Tiles(t)) {
		t.Fatal("late swap mutated the board")
	}
}

// TestSwapFirstAppliesBeforeDeadline Swap 先行入队：在窗口内正常结算，
// 随后截止命令才流转回合——先到先判，结果确定。
func TestSwapFirstAppliesBeforeDeadline(t *testing.T) {
	frames := runRoom(t, room.DefaultConfig(), testDeps(t, 0, fullScript(t)), func(r *room.Room) {
		r.SubmitSwap(0, swapA) // 先于截止到达：回合 1 内有效
		closeAllTurns(r, 12)
	})
	swapAt := slices.Index(kinds(frames), room.KindSwapResult)
	turn2 := -1
	for i, f := range frames {
		if v, ok := f.(room.TurnStartedFrame); ok && v.Turn == 2 {
			turn2 = i
			break
		}
	}
	if swapAt == -1 || turn2 == -1 || swapAt > turn2 {
		t.Fatalf("expected swap_result before turn_started(2), got swap@%d turn2@%d", swapAt, turn2)
	}
	res := frames[swapAt].(room.SwapResultFrame)
	if res.Operator != 0 || res.Scores != [2]int{3, 0} {
		t.Fatalf("in-window swap = %+v, want operator 0 scoring [3 0]", res)
	}
}

// TestStaleDeadlineIgnored 非当前回合的截止命令被忽略，不产生任何帧。
func TestStaleDeadlineIgnored(t *testing.T) {
	frames := runRoom(t, room.DefaultConfig(), testDeps(t, 0, board1Tiles(t)), func(r *room.Room) {
		r.SubmitDeadline(2) // 迟到：当前仍是回合 1
		closeAllTurns(r, 12)
	})
	want := []room.FrameKind{room.KindMatchStarted}
	for i := 0; i < 11; i++ {
		want = append(want, room.KindTurnStarted)
	}
	want = append(want, room.KindSettlement)
	if !slices.Equal(kinds(frames), want) {
		t.Fatalf("frame kinds = %v, want only snapshots and settlement", kinds(frames))
	}
	if snap := snapshotOf(t, frames[1]); snap.Turn != 2 {
		t.Fatalf("first turn snapshot = turn %d, want 2", snap.Turn)
	}
}

// ---- 配置项 ----

// TestConfigurableTiming 回合秒数与总回合数为配置项。
func TestConfigurableTiming(t *testing.T) {
	cfg := room.DefaultConfig()
	cfg.TurnSeconds = 2
	cfg.TotalTurns = 3

	frames := runRoom(t, cfg, testDeps(t, 1, board1Tiles(t)), func(r *room.Room) {
		closeAllTurns(r, 3)
	})

	want := []room.FrameKind{room.KindMatchStarted, room.KindTurnStarted, room.KindTurnStarted, room.KindSettlement}
	if !slices.Equal(kinds(frames), want) {
		t.Fatalf("frame kinds = %v, want 3-turn game", kinds(frames))
	}
	wantDeadline := baseTime.Add(2 * time.Second)
	for i := 0; i < 3; i++ {
		snap := snapshotOf(t, frames[i])
		if !snap.Deadline.Equal(wantDeadline) {
			t.Fatalf("snapshot %d deadline = %v, want %v", i, snap.Deadline, wantDeadline)
		}
	}
	// 先手为座位 1：回合 2 操作方回到座位 0。
	if snap := snapshotOf(t, frames[1]); snap.Operator != 0 {
		t.Fatalf("turn 2 operator = %d, want 0", snap.Operator)
	}
}

// ---- 并发提交：串行化与无数据竞态 ----

// TestConcurrentSubmitNoRace 多 goroutine 并发投递 Swap 与截止命令，
// 全部按到达顺序串行处理：-race 下无数据竞态，记账与结算自洽。
func TestConcurrentSubmitNoRace(t *testing.T) {
	// TileRNG 只被事件循环 goroutine 调用，可持有独立 PCG 源；
	// 投递方 goroutine 共用 math/rand/v2 顶层函数（并发安全）。
	rng := rand.New(rand.NewPCG(42, 42))
	var sinkCount int // 仅事件循环写入；测试在 <-done 之后读取
	r := room.New([2]string{"P0", "P1"}, room.DefaultConfig(), room.Deps{
		TileRNG:   func(colors int) judge.Tile { return judge.Tile(rng.IntN(colors)) },
		FirstCoin: func() int { return 0 },
		Now:       func() time.Time { return baseTime },
	}, [2]room.Sink{
		func(f room.Frame) { sinkCount++ },
		func(room.Frame) {},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run()
	}()

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				a := judge.Pos{Col: rand.IntN(9), Row: rand.IntN(9)}
				b := a
				if rand.IntN(2) == 0 {
					if a.Col+1 < 9 {
						b.Col++
					} else {
						b.Col--
					}
				} else {
					if a.Row+1 < 9 {
						b.Row++
					} else {
						b.Row--
					}
				}
				r.SubmitSwap(rand.IntN(2), judge.Swap{A: a, B: b})
			}
			for i := 0; i < 4; i++ {
				r.SubmitDeadline(1)
			}
		}()
	}
	wg.Wait()
	closeAllTurns(r, 12) // 收尾：依次关闭所有回合直达结算
	<-done

	if sinkCount == 0 {
		t.Fatal("no frames emitted")
	}
}
