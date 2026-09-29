package hub_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"poptrio/server/internal/hub"
	"poptrio/server/internal/judge"
	"poptrio/server/internal/protocol"
	"poptrio/server/internal/room"
)

// ---- 脚本化 WS 客户端 ----

// wsClient 是测试用脚本 WS 客户端：收发带超时，断言类型与内容。
type wsClient struct {
	t *testing.T
	c *websocket.Conn
}

func dialWS(t *testing.T, baseURL, path string) *wsClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(baseURL, "http") + path
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return &wsClient{t: t, c: c}
}

func (cl *wsClient) sendRaw(data string) {
	cl.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cl.c.Write(ctx, websocket.MessageText, []byte(data)); err != nil {
		cl.t.Fatalf("send %s: %v", data, err)
	}
}

func (cl *wsClient) sendSwap(sw judge.Swap) {
	cl.t.Helper()
	payload, err := json.Marshal(protocol.Swap{
		A: protocol.Pos{Col: sw.A.Col, Row: sw.A.Row},
		B: protocol.Pos{Col: sw.B.Col, Row: sw.B.Row},
	})
	if err != nil {
		cl.t.Fatal(err)
	}
	cl.sendRaw(`{"v":1,"type":"swap","payload":` + string(payload) + `}`)
}

// recv 收一条消息，返回类型与载荷。
func (cl *wsClient) recv() (string, json.RawMessage) {
	cl.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := cl.c.Read(ctx)
	if err != nil {
		cl.t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		cl.t.Fatalf("read: got binary message, want text")
	}
	var env struct {
		V       int             `json:"v"`
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		cl.t.Fatalf("server frame is not JSON: %v (%s)", err, data)
	}
	if env.V != protocol.Version {
		cl.t.Fatalf("server frame version = %d, want %d", env.V, protocol.Version)
	}
	return env.Type, env.Payload
}

// expect 等待指定类型的消息，跳过 skip 中的类型（如 reshuffle）。
func (cl *wsClient) expect(want string, skip ...string) json.RawMessage {
	cl.t.Helper()
	for {
		typ, payload := cl.recv()
		if typ == want {
			return payload
		}
		if !slices.Contains(skip, typ) {
			cl.t.Fatalf("got %s, want %s", typ, want)
		}
	}
}

// expectError 等待错误帧并断言错误码。
func (cl *wsClient) expectError(code string) {
	cl.t.Helper()
	payload := cl.expect(protocol.TypeError)
	var msg protocol.ErrorMsg
	if err := json.Unmarshal(payload, &msg); err != nil {
		cl.t.Fatalf("error payload: %v", err)
	}
	if msg.Code != code {
		cl.t.Fatalf("error code = %s, want %s", msg.Code, code)
	}
}

// expectClose 断言服务端关闭了连接（settlement 之后的正常关闭）。
func (cl *wsClient) expectClose() {
	cl.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := cl.c.Read(ctx); err == nil {
		cl.t.Fatal("connection still open after settlement")
	}
}

// findValidSwap 白盒枚举相邻交换，返回第一个能成线的交换。
func findValidSwap(t *testing.T, board []int) (judge.Swap, bool) {
	t.Helper()
	cfg := judge.DefaultConfig()
	if len(board) != cfg.Width*cfg.Height {
		t.Fatalf("board has %d tiles, want %d", len(board), cfg.Width*cfg.Height)
	}
	for row := 0; row < cfg.Height; row++ {
		for col := 0; col < cfg.Width; col++ {
			for _, q := range []judge.Pos{{Col: col + 1, Row: row}, {Col: col, Row: row + 1}} {
				if q.Col >= cfg.Width || q.Row >= cfg.Height {
					continue
				}
				p := judge.Pos{Col: col, Row: row}
				tiles := make([]judge.Tile, len(board))
				for i, v := range board {
					tiles[i] = judge.Tile(v)
				}
				i := p.Row*cfg.Width + p.Col
				j := q.Row*cfg.Width + q.Col
				tiles[i], tiles[j] = tiles[j], tiles[i]
				b, err := judge.NewBoard(cfg, tiles)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := b.FindClear(); ok {
					return judge.Swap{A: p, B: q}, true
				}
			}
		}
	}
	return judge.Swap{}, false
}

// ---- 端到端冒烟：排队 → 配对 → 完整对局 → Settlement ----

// TestScriptedClientsFullMatch 两个脚本 WS 客户端从排队到终局走完整局：
// 小配置（2 回合 x 1 秒）下双方各打一次有效交换，回合计时器自动流转，
// 双方收到逐帧一致的 Settlement，随后服务端关闭连接。
func TestScriptedClientsFullMatch(t *testing.T) {
	cfg := hub.Config{
		Room:         room.Config{Board: judge.DefaultConfig(), TurnSeconds: 1, TotalTurns: 2, GraceSeconds: 1},
		QueueTimeout: 10 * time.Second,
	}
	h := hub.New(cfg, room.Deps{FirstCoin: func() int { return 0 }}) // 固定先手座位 0
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	c1 := dialWS(t, srv.URL, "/ws")
	c2 := dialWS(t, srv.URL, "/ws")

	// 排队：先入队者收到确认；第二人入队即配对。
	c1.sendRaw(`{"v":1,"type":"queue_join"}`)
	c1.expect(protocol.TypeQueueJoined)
	c2.sendRaw(`{"v":1,"type":"queue_join"}`)
	c2.expect(protocol.TypeQueueJoined)

	// 配对后各自先收到私有 match_token（绝不广播），再收到 match_started。
	var tok1, tok2 protocol.MatchTokenMsg
	if err := json.Unmarshal(c1.expect(protocol.TypeMatchToken), &tok1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(c2.expect(protocol.TypeMatchToken), &tok2); err != nil {
		t.Fatal(err)
	}
	if tok1.Token == "" || tok2.Token == "" || tok1.Token == tok2.Token {
		t.Fatalf("match tokens = %q / %q, want non-empty and distinct", tok1.Token, tok2.Token)
	}
	if tok1.Seat != 0 || tok2.Seat != 1 {
		t.Fatalf("token seats = %d / %d, want 0 / 1", tok1.Seat, tok2.Seat)
	}

	// 双方收到 match_started：座位表、先手与回合 1 快照，快照逐字段一致。
	var m1, m2 protocol.MatchStartedMsg
	if err := json.Unmarshal(c1.expect(protocol.TypeMatchStarted), &m1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(c2.expect(protocol.TypeMatchStarted), &m2); err != nil {
		t.Fatal(err)
	}
	if m1.FirstSeat != 0 || m1.Seats[0] == m1.Seats[1] {
		t.Fatalf("match started = %+v, want distinct seats with first seat 0", m1)
	}
	if !reflect.DeepEqual(m1.Snapshot, m2.Snapshot) {
		t.Fatalf("snapshots differ: %+v vs %+v", m1.Snapshot, m2.Snapshot)
	}

	// 回合 1：先手提交一个有效交换（房间不变量保证回合开始快照必有可行
	// 交换：死局在流转前已被重排），双方收到一致的 swap_result。
	sw, ok := findValidSwap(t, m1.Snapshot.Board)
	if !ok {
		t.Fatal("no valid swap on turn-1 snapshot (room invariant broken)")
	}
	c1.sendSwap(sw)
	var r1, r2 protocol.SwapResultMsg
	if err := json.Unmarshal(c1.expect(protocol.TypeSwapResult, protocol.TypeReshuffle), &r1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(c2.expect(protocol.TypeSwapResult, protocol.TypeReshuffle), &r2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r1, r2) || r1.Operator != 0 {
		t.Fatalf("swap results differ or wrong operator: %+v vs %+v", r1, r2)
	}

	// 回合计时器自动流转：1 秒窗口后双方收到回合 2 快照（操作方座位 1）。
	var t1, t2 protocol.TurnStartedMsg
	if err := json.Unmarshal(c1.expect(protocol.TypeTurnStarted, protocol.TypeReshuffle), &t1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(c2.expect(protocol.TypeTurnStarted, protocol.TypeReshuffle), &t2); err != nil {
		t.Fatal(err)
	}
	if t1.Snapshot.Turn != 2 || t1.Snapshot.Operator != 1 {
		t.Fatalf("turn 2 snapshot = %+v, want turn 2 operator 1", t1.Snapshot)
	}
	if !reflect.DeepEqual(t1.Snapshot, t2.Snapshot) {
		t.Fatalf("turn 2 snapshots differ: %+v vs %+v", t1.Snapshot, t2.Snapshot)
	}

	// 回合 2：座位 1 提交一个有效交换。
	sw2, ok := findValidSwap(t, t1.Snapshot.Board)
	if !ok {
		t.Fatal("no valid swap on turn-2 snapshot (room invariant broken)")
	}
	c2.sendSwap(sw2)
	c1.expect(protocol.TypeSwapResult, protocol.TypeReshuffle)
	c2.expect(protocol.TypeSwapResult, protocol.TypeReshuffle)

	// 窗口到期 → 终局结算，双方一致；随后服务端关闭连接。
	var s1, s2 protocol.SettlementMsg
	if err := json.Unmarshal(c1.expect(protocol.TypeSettlement, protocol.TypeReshuffle), &s1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(c2.expect(protocol.TypeSettlement, protocol.TypeReshuffle), &s2); err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatalf("settlements differ: %+v vs %+v", s1, s2)
	}
	c1.expectClose()
	c2.expectClose()
}

// TestDisconnectGraceForfeit 对局中掉线：宽限期（此处 1 秒）届满未归，
// 对方收到判负 Settlement——issue 04 语义经协议层接线兑现。
func TestDisconnectGraceForfeit(t *testing.T) {
	cfg := hub.Config{
		// 宽限 1 秒 < 回合窗口 3 秒：判负结算先于回合流转，时序确定。
		Room:         room.Config{Board: judge.DefaultConfig(), TurnSeconds: 3, TotalTurns: 12, GraceSeconds: 1},
		QueueTimeout: 10 * time.Second,
	}
	h := hub.New(cfg, room.Deps{FirstCoin: func() int { return 0 }})
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	c1 := dialWS(t, srv.URL, "/ws")
	c2 := dialWS(t, srv.URL, "/ws")
	c1.sendRaw(`{"v":1,"type":"queue_join"}`)
	c1.expect(protocol.TypeQueueJoined)
	c2.sendRaw(`{"v":1,"type":"queue_join"}`)
	c2.expect(protocol.TypeQueueJoined)
	c1.expect(protocol.TypeMatchToken)
	c2.expect(protocol.TypeMatchToken)
	c1.expect(protocol.TypeMatchStarted)
	c2.expect(protocol.TypeMatchStarted)

	// 座位 1 掉线：连接直接断开，不发送任何消息。
	c2.c.CloseNow()

	// 宽限 1 秒届满：存活方收到判负结算（胜方 = 座位 0）。
	var settle protocol.SettlementMsg
	if err := json.Unmarshal(c1.expect(protocol.TypeSettlement, protocol.TypeReshuffle), &settle); err != nil {
		t.Fatal(err)
	}
	if settle.Draw || settle.Winner != 0 {
		t.Fatalf("settlement = %+v, want forfeit win for seat 0", settle)
	}
}

// TestReconnectResumesMatch 座位 1 掉线后凭对局 token 重连：宽限期内恢复
// 成功、对手收到 reconnected 广播、对局照常打满并结算（issue 04 语义）。
func TestReconnectResumesMatch(t *testing.T) {
	cfg := hub.Config{
		Room:         room.Config{Board: judge.DefaultConfig(), TurnSeconds: 3, TotalTurns: 2, GraceSeconds: 2},
		QueueTimeout: 10 * time.Second,
	}
	h := hub.New(cfg, room.Deps{FirstCoin: func() int { return 0 }})
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	c1 := dialWS(t, srv.URL, "/ws")
	c2 := dialWS(t, srv.URL, "/ws")
	c1.sendRaw(`{"v":1,"type":"queue_join"}`)
	c1.expect(protocol.TypeQueueJoined)
	c2.sendRaw(`{"v":1,"type":"queue_join"}`)
	c2.expect(protocol.TypeQueueJoined)
	var tok2 protocol.MatchTokenMsg
	if err := json.Unmarshal(c2.expect(protocol.TypeMatchToken), &tok2); err != nil {
		t.Fatal(err)
	}
	c1.expect(protocol.TypeMatchToken)
	c1.expect(protocol.TypeMatchStarted)
	c2.expect(protocol.TypeMatchStarted)

	// 座位仍被占用时重连：拒绝。
	c3 := dialWS(t, srv.URL, "/ws")
	c3.sendRaw(`{"v":1,"type":"reconnect","payload":{"token":"` + tok2.Token + `"}}`)
	c3.expectError(protocol.CodeBadToken)

	// 座位 1 掉线后凭 token 重连：新连接收到全量快照。
	c2.c.CloseNow()
	time.Sleep(250 * time.Millisecond) // 等待服务端异步清理旧连接（换绑的前提）
	c3.sendRaw(`{"v":1,"type":"reconnect","payload":{"token":"` + tok2.Token + `"}}`)
	var rec protocol.ReconnectedMsg
	if err := json.Unmarshal(c3.expect(protocol.TypeReconnected), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Seat != 1 || rec.Snapshot.Turn != 1 {
		t.Fatalf("reconnected = %+v, want seat 1 turn 1 snapshot", rec)
	}
	c1.expect(protocol.TypeReconnected) // 对手收到重连广播

	// 恢复后对局照常：回合 1 座位 0 交换，回合 2 座位 1（c3）交换，正常结算。
	sw, ok := findValidSwap(t, rec.Snapshot.Board)
	if !ok {
		t.Fatal("no valid swap on snapshot (room invariant broken)")
	}
	c1.sendSwap(sw)
	c1.expect(protocol.TypeSwapResult, protocol.TypeReshuffle)
	c3.expect(protocol.TypeSwapResult, protocol.TypeReshuffle)

	var t1 protocol.TurnStartedMsg
	if err := json.Unmarshal(c3.expect(protocol.TypeTurnStarted, protocol.TypeReshuffle), &t1); err != nil {
		t.Fatal(err)
	}
	if t1.Snapshot.Turn != 2 || t1.Snapshot.Operator != 1 {
		t.Fatalf("turn 2 snapshot = %+v, want turn 2 operator 1", t1.Snapshot)
	}
	c1.expect(protocol.TypeTurnStarted, protocol.TypeReshuffle)

	sw2, ok := findValidSwap(t, t1.Snapshot.Board)
	if !ok {
		t.Fatal("no valid swap on turn-2 snapshot (room invariant broken)")
	}
	c3.sendSwap(sw2)
	c1.expect(protocol.TypeSwapResult, protocol.TypeReshuffle)
	c3.expect(protocol.TypeSwapResult, protocol.TypeReshuffle)

	c1.expect(protocol.TypeSettlement, protocol.TypeReshuffle)
	c3.expect(protocol.TypeSettlement, protocol.TypeReshuffle)
}

// ---- 排队超时与手动取消 ----

func TestQueueTimeoutE2E(t *testing.T) {
	cfg := hub.Config{Room: room.DefaultConfig(), QueueTimeout: 100 * time.Millisecond}
	srv := httptest.NewServer(hub.New(cfg, room.Deps{}).Handler())
	t.Cleanup(srv.Close)

	c := dialWS(t, srv.URL, "/ws")
	c.sendRaw(`{"v":1,"type":"queue_join"}`)
	c.expect(protocol.TypeQueueJoined)
	c.expect(protocol.TypeQueueTimeout) // 约 100ms 后自动退出队列
}

func TestQueueCancelE2E(t *testing.T) {
	cfg := hub.Config{Room: room.DefaultConfig(), QueueTimeout: time.Minute}
	srv := httptest.NewServer(hub.New(cfg, room.Deps{}).Handler())
	t.Cleanup(srv.Close)

	c := dialWS(t, srv.URL, "/ws")
	c.sendRaw(`{"v":1,"type":"queue_join"}`)
	c.expect(protocol.TypeQueueJoined)
	c.sendRaw(`{"v":1,"type":"queue_cancel"}`)
	c.expect(protocol.TypeQueueCancelled)

	// 取消后可重新排队。
	c.sendRaw(`{"v":1,"type":"queue_join"}`)
	c.expect(protocol.TypeQueueJoined)
	c.sendRaw(`{"v":1,"type":"queue_cancel"}`)
	c.expect(protocol.TypeQueueCancelled)
}

// ---- 非法消息：明确错误帧，进程不崩溃 ----

func TestInvalidMessagesE2E(t *testing.T) {
	cfg := hub.Config{Room: room.DefaultConfig(), QueueTimeout: time.Minute}
	srv := httptest.NewServer(hub.New(cfg, room.Deps{}).Handler())
	t.Cleanup(srv.Close)

	c := dialWS(t, srv.URL, "/ws")
	for _, tt := range []struct {
		data string
		code string
	}{
		{`{nope`, protocol.CodeBadJSON},
		{`{"type":"queue_join"}`, protocol.CodeBadVersion},
		{`{"v":2,"type":"queue_join"}`, protocol.CodeBadVersion},
		{`{"v":1,"type":"teleport"}`, protocol.CodeUnknownType},
		{`{"v":1,"type":"swap"}`, protocol.CodeBadPayload},
		{`{"v":1,"type":"swap","payload":{"a":{"col":0,"row":0}}}`, protocol.CodeBadPayload},
		{`{"v":1,"type":"swap","payload":{"a":{"col":0,"row":0},"b":{"col":1,"row":0}}}`, protocol.CodeBadState},
		{`{"v":1,"type":"queue_cancel"}`, protocol.CodeBadState},
	} {
		c.sendRaw(tt.data)
		c.expectError(tt.code)
	}

	// 错误风暴后连接仍可用：正常排队、取消全流程。
	c.sendRaw(`{"v":1,"type":"queue_join"}`)
	c.expect(protocol.TypeQueueJoined)
	c.sendRaw(`{"v":1,"type":"queue_cancel"}`)
	c.expect(protocol.TypeQueueCancelled)
}

// ---- HTTP 健康检查 ----

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(hub.New(hub.Config{QueueTimeout: time.Minute}, room.Deps{}).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}
