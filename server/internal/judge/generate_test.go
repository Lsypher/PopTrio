package judge_test

import (
	"slices"
	"testing"

	"poptrio/server/internal/judge"
)

// assertPlayable 断言棋盘满足生成的两条约束：无现成连线、至少存在一步有效交换。
func assertPlayable(t *testing.T, b judge.Board) {
	t.Helper()
	if len(b.Tiles) != b.Config.Width*b.Config.Height {
		t.Fatalf("board has %d tiles, want %d", len(b.Tiles), b.Config.Width*b.Config.Height)
	}
	if _, lined := b.FindClear(); lined {
		t.Fatalf("generated board has pre-existing lines: %+v", b.FindLines())
	}
	if b.IsDeadBoard() {
		t.Fatal("generated board has no valid swap")
	}
}

// ---- 初始棋盘 / Reshuffle 生成 ----

func TestGenerateBoardSatisfiesBothConstraints(t *testing.T) {
	// 统计性验证：多次生成全部满足两约束。
	rand := pcgSource(42)
	for i := 0; i < 200; i++ {
		b, err := judge.GenerateBoard(judge.DefaultConfig(), rand)
		if err != nil {
			t.Fatalf("board %d: GenerateBoard: %v", i, err)
		}
		assertPlayable(t, b)
	}
}

func TestGenerateBoardHonorsInjectedConfig(t *testing.T) {
	rand := pcgSource(7)
	for i := 0; i < 50; i++ {
		b, err := judge.GenerateBoard(judge.Config{Width: 4, Height: 4, Colors: 3}, rand)
		if err != nil {
			t.Fatalf("board %d: GenerateBoard: %v", i, err)
		}
		assertPlayable(t, b)
	}
}

func TestGenerateBoardIsDeterministicGivenRNG(t *testing.T) {
	a, errA := judge.GenerateBoard(judge.DefaultConfig(), pcgSource(7))
	b, errB := judge.GenerateBoard(judge.DefaultConfig(), pcgSource(7))
	if errA != nil || errB != nil {
		t.Fatalf("GenerateBoard errors: %v / %v", errA, errB)
	}
	if !slices.Equal(a.Tiles, b.Tiles) {
		t.Fatal("same seed produced different boards")
	}
}

func TestGenerateBoardReshufflesDeadBoard(t *testing.T) {
	// Reshuffle 与初始生成共用 GenerateBoard：以 bg9 死局为输入，
	// 生成结果必须满足两约束（可继续对局）。
	dead := painted9(t)
	if !dead.IsDeadBoard() {
		t.Fatal("fixture broken: bg9 pattern should be a dead board")
	}
	fresh, err := judge.GenerateBoard(judge.DefaultConfig(), pcgSource(99))
	if err != nil {
		t.Fatal(err)
	}
	assertPlayable(t, fresh)
	if slices.Equal(fresh.Tiles, dead.Tiles) {
		t.Fatal("reshuffled board equals the dead board")
	}
}

func TestGenerateBoardRejectsImpossibleRequests(t *testing.T) {
	t.Run("invalid config", func(t *testing.T) {
		if _, err := judge.GenerateBoard(judge.Config{Width: 0, Height: 9, Colors: 6}, pcgSource(1)); err == nil {
			t.Fatal("GenerateBoard with zero width = nil error, want error")
		}
	})
	t.Run("single color can never be line-free", func(t *testing.T) {
		if _, err := judge.GenerateBoard(judge.Config{Width: 4, Height: 4, Colors: 1}, pcgSource(1)); err == nil {
			t.Fatal("GenerateBoard with 1 color = nil error, want error")
		}
	})
}
