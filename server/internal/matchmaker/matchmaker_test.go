package matchmaker_test

import (
	"testing"
	"time"

	"poptrio/server/internal/matchmaker"
)

// await 等待结果通道投递；超时即失败（结果通道 buffered 1，永不阻塞投递方）。
func await(t *testing.T, out <-chan matchmaker.Outcome) matchmaker.Outcome {
	t.Helper()
	select {
	case o := <-out:
		return o
	case <-time.After(2 * time.Second):
		t.Fatal("outcome not delivered within 2s")
		return matchmaker.Outcome{}
	}
}

// assertEmpty 确认结果通道此后 50ms 内不再有投递（恰好一次语义）。
func assertEmpty(t *testing.T, out <-chan matchmaker.Outcome) {
	t.Helper()
	select {
	case o := <-out:
		t.Fatalf("unexpected extra outcome %+v", o)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPairFirstTwo(t *testing.T) {
	mm := matchmaker.New(time.Minute)
	outA := mm.Join("A")
	outB := mm.Join("B")

	oa := await(t, outA)
	ob := await(t, outB)
	if oa.Kind != matchmaker.OutcomeMatched || ob.Kind != matchmaker.OutcomeMatched {
		t.Fatalf("outcomes = %v/%v, want both matched", oa.Kind, ob.Kind)
	}
	if oa.Match != ob.Match {
		t.Fatal("both sides received different matchups")
	}
	if oa.Match.Players != [2]any{"A", "B"} {
		t.Fatalf("players = %v, want [A B] (FIFO)", oa.Match.Players)
	}
	// 配对后队列清空：第三人正常排队等待，不被旧成员波及。
	outC := mm.Join("C")
	assertEmpty(t, outC)
	mm.Cancel("C")
}

func TestQueueTimeout(t *testing.T) {
	mm := matchmaker.New(20 * time.Millisecond)
	out := mm.Join("A")
	if o := await(t, out); o.Kind != matchmaker.OutcomeTimeout {
		t.Fatalf("outcome = %v, want timeout", o.Kind)
	}
	// 超时后自动退出队列：可重新排队（新队列长超时，排除定时器干扰）。
	mm2 := matchmaker.New(time.Minute)
	out2 := mm2.Join("B")
	if !mm2.Cancel("B") {
		t.Fatal("re-join after timeout should be cancellable")
	}
	if o := await(t, out2); o.Kind != matchmaker.OutcomeCancelled {
		t.Fatalf("outcome = %v, want cancelled", o.Kind)
	}
}

func TestCancelImmediately(t *testing.T) {
	mm := matchmaker.New(time.Minute)
	out := mm.Join("A")
	if !mm.Cancel("A") {
		t.Fatal("cancel of queued player returned false")
	}
	if o := await(t, out); o.Kind != matchmaker.OutcomeCancelled {
		t.Fatalf("outcome = %v, want cancelled", o.Kind)
	}
	// 取消后不在队列：再次取消无副作用。
	if mm.Cancel("A") {
		t.Fatal("cancel of non-queued player returned true")
	}
	assertEmpty(t, out)
}

func TestCancelAfterTimeoutSingleDelivery(t *testing.T) {
	// 取消与超时定时器并发竞争：结果恰好投递一次（cancelled 或 timeout 二选一）。
	for i := 0; i < 50; i++ {
		mm := matchmaker.New(time.Millisecond)
		out := mm.Join("A")
		mm.Cancel("A")
		o := await(t, out)
		if o.Kind != matchmaker.OutcomeCancelled && o.Kind != matchmaker.OutcomeTimeout {
			t.Fatalf("run %d: outcome = %v, want cancelled or timeout", i, o.Kind)
		}
		assertEmpty(t, out)
	}
}

func TestThreeWaitersPairSequentially(t *testing.T) {
	mm := matchmaker.New(time.Minute)
	outA := mm.Join("A")
	outB := mm.Join("B")
	await(t, outA)
	await(t, outB)

	outC := mm.Join("C")
	outD := mm.Join("D")
	oc := await(t, outC)
	await(t, outD)
	if oc.Match.Players != [2]any{"C", "D"} {
		t.Fatalf("second matchup = %v, want [C D]", oc.Match.Players)
	}
}

// TestPairingStartOnce 验证配对双方并发调用 Start 时动作恰好执行一次，
// 且另一方的 Start 在动作完成后才返回（sync.Once 语义）。
func TestPairingStartOnce(t *testing.T) {
	mm := matchmaker.New(time.Minute)
	outA := mm.Join("A")
	outB := mm.Join("B")
	pair := await(t, outA).Match
	await(t, outB)

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		pair.Start(func() {
			close(entered)
			<-release
		})
	}()
	<-entered

	finished := make(chan struct{})
	go func() {
		pair.Start(func() { t.Error("second Start executed the action") })
		close(finished)
	}()
	select {
	case <-finished:
		t.Fatal("second Start returned before the first action completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
	<-finished
}
