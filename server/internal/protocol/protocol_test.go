package protocol_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"poptrio/server/internal/judge"
	"poptrio/server/internal/protocol"
	"poptrio/server/internal/room"
)

// ---- Parse：入站消息 ----

func TestParseSwap(t *testing.T) {
	in, err := protocol.Parse([]byte(`{"v":1,"type":"swap","payload":{"a":{"col":2,"row":0},"b":{"col":3,"row":0}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if in.Type != protocol.TypeSwap {
		t.Fatalf("type = %s, want swap", in.Type)
	}
	want := protocol.Swap{A: protocol.Pos{Col: 2, Row: 0}, B: protocol.Pos{Col: 3, Row: 0}}
	if in.Swap != want {
		t.Fatalf("swap = %+v, want %+v", in.Swap, want)
	}
}

func TestParseQueueMessages(t *testing.T) {
	for _, typ := range []string{protocol.TypeQueueJoin, protocol.TypeQueueCancel} {
		in, err := protocol.Parse([]byte(`{"v":1,"type":"` + typ + `"}`))
		if err != nil {
			t.Fatalf("parse %s: %v", typ, err)
		}
		if in.Type != typ {
			t.Fatalf("type = %s, want %s", in.Type, typ)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		want error
	}{
		{"bad json", `{nope`, protocol.ErrBadJSON},
		{"missing version", `{"type":"queue_join"}`, protocol.ErrBadVersion},
		{"zero version", `{"v":0,"type":"queue_join"}`, protocol.ErrBadVersion},
		{"future version", `{"v":2,"type":"queue_join"}`, protocol.ErrBadVersion},
		{"unknown type", `{"v":1,"type":"teleport"}`, protocol.ErrUnknownType},
		{"swap missing a", `{"v":1,"type":"swap","payload":{"b":{"col":1,"row":1}}}`, protocol.ErrBadPayload},
		{"swap missing b", `{"v":1,"type":"swap","payload":{"a":{"col":1,"row":1}}}`, protocol.ErrBadPayload},
		{"swap payload not object", `{"v":1,"type":"swap","payload":[1,2]}`, protocol.ErrBadPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := protocol.Parse([]byte(tt.data))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// ---- Encode：出站消息 ----

func TestEncodeEnvelope(t *testing.T) {
	data, err := protocol.Encode(protocol.TypeQueueJoined, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		V       int             `json:"v"`
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("encoded frame is not JSON: %v (%s)", err, data)
	}
	if env.V != 1 || env.Type != protocol.TypeQueueJoined {
		t.Fatalf("envelope = v%d type %s, want v1 type queue_joined", env.V, env.Type)
	}
}

// ---- FromRoom：房间帧 → 线上消息 ----

// roomRoundTrip 把房间帧经 FromRoom + Encode 编码后再解析回线上结构，
// 断言逐字段保真。这是 hub 侧唯一的帧出站路径。
func roomRoundTrip(t *testing.T, f room.Frame) (string, json.RawMessage) {
	t.Helper()
	typ, payload := protocol.FromRoom(f)
	data, err := protocol.Encode(typ, payload)
	if err != nil {
		t.Fatalf("encode %s: %v", typ, err)
	}
	var env struct {
		V       int             `json:"v"`
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("encoded %s frame is not JSON: %v", typ, err)
	}
	if env.V != 1 || env.Type != typ {
		t.Fatalf("envelope = v%d type %s, want v1 type %s", env.V, env.Type, typ)
	}
	return typ, env.Payload
}

func TestFromRoomMatchStarted(t *testing.T) {
	snap := room.Snapshot{
		Board:    []judge.Tile{0, 1, 2, 3},
		Scores:   [2]int{3, 0},
		Turn:     1,
		Operator: 1,
		Deadline: time.Date(2026, 9, 28, 12, 0, 10, 0, time.UTC),
	}
	typ, payload := roomRoundTrip(t, room.MatchStartedFrame{
		Seats:     [2]string{"p1", "p2"},
		FirstSeat: 1,
		Snapshot:  snap,
	})
	if typ != protocol.TypeMatchStarted {
		t.Fatalf("type = %s, want match_started", typ)
	}
	var msg struct {
		Seats     [2]string         `json:"seats"`
		FirstSeat int               `json:"first_seat"`
		Snapshot  protocol.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Seats != [2]string{"p1", "p2"} || msg.FirstSeat != 1 {
		t.Fatalf("seats/first = %+v/%d, want [p1 p2]/1", msg.Seats, msg.FirstSeat)
	}
	if !slices.Equal(msg.Snapshot.Board, []int{0, 1, 2, 3}) {
		t.Fatalf("board = %v, want [0 1 2 3]", msg.Snapshot.Board)
	}
	if msg.Snapshot.Scores != [2]int{3, 0} || msg.Snapshot.Turn != 1 || msg.Snapshot.Operator != 1 {
		t.Fatalf("snapshot = %+v, want scores [3 0] turn 1 operator 1", msg.Snapshot)
	}
	if !msg.Snapshot.Deadline.Equal(snap.Deadline) {
		t.Fatalf("deadline = %v, want %v", msg.Snapshot.Deadline, snap.Deadline)
	}
}

func TestFromRoomTurnStarted(t *testing.T) {
	typ, payload := roomRoundTrip(t, room.TurnStartedFrame{Snapshot: room.Snapshot{
		Board: []judge.Tile{5, 4}, Scores: [2]int{0, 2}, Turn: 7, Operator: 0,
		Deadline: time.Date(2026, 9, 28, 12, 0, 20, 0, time.UTC),
	}})
	if typ != protocol.TypeTurnStarted {
		t.Fatalf("type = %s, want turn_started", typ)
	}
	var msg struct {
		Snapshot protocol.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Snapshot.Turn != 7 || msg.Snapshot.Operator != 0 || msg.Snapshot.Scores != [2]int{0, 2} {
		t.Fatalf("snapshot = %+v, want turn 7 operator 0 scores [0 2]", msg.Snapshot)
	}
	if !slices.Equal(msg.Snapshot.Board, []int{5, 4}) {
		t.Fatalf("board = %v, want [5 4]", msg.Snapshot.Board)
	}
}

func TestFromRoomSwapResult(t *testing.T) {
	typ, payload := roomRoundTrip(t, room.SwapResultFrame{
		Operator: 1,
		Swap:     judge.Swap{A: judge.Pos{Col: 4, Row: 2}, B: judge.Pos{Col: 4, Row: 3}},
		Waves: []room.WaveView{
			{Clear: []judge.Pos{{Col: 0, Row: 0}, {Col: 1, Row: 0}}, Score: 2, Board: []judge.Tile{1, 1}},
		},
		Board:  []judge.Tile{1, 1},
		Scores: [2]int{0, 2},
	})
	if typ != protocol.TypeSwapResult {
		t.Fatalf("type = %s, want swap_result", typ)
	}
	var msg struct {
		Operator int             `json:"operator"`
		Swap     protocol.Swap   `json:"swap"`
		Waves    []protocol.Wave `json:"waves"`
		Board    []int           `json:"board"`
		Scores   [2]int          `json:"scores"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Operator != 1 || msg.Scores != [2]int{0, 2} || !slices.Equal(msg.Board, []int{1, 1}) {
		t.Fatalf("swap result = %+v, want operator 1 scores [0 2] board [1 1]", msg)
	}
	if msg.Swap != (protocol.Swap{A: protocol.Pos{Col: 4, Row: 2}, B: protocol.Pos{Col: 4, Row: 3}}) {
		t.Fatalf("swap echo = %+v, want (4,2)<->(4,3)", msg.Swap)
	}
	if len(msg.Waves) != 1 {
		t.Fatalf("waves = %d, want 1", len(msg.Waves))
	}
	w := msg.Waves[0]
	if w.Score != 2 || !slices.Equal(w.Clear, []protocol.Pos{{Col: 0, Row: 0}, {Col: 1, Row: 0}}) {
		t.Fatalf("wave = %+v, want score 2 clear [(0,0) (1,0)]", w)
	}
}

func TestFromRoomSwapRejected(t *testing.T) {
	typ, payload := roomRoundTrip(t, room.SwapRejectedFrame{
		Seat:   0,
		Swap:   judge.Swap{A: judge.Pos{Col: 2, Row: 0}, B: judge.Pos{Col: 3, Row: 0}},
		Reason: room.RejectNoClear,
	})
	if typ != protocol.TypeSwapRejected {
		t.Fatalf("type = %s, want swap_rejected", typ)
	}
	var msg struct {
		Seat   int           `json:"seat"`
		Swap   protocol.Swap `json:"swap"`
		Reason string        `json:"reason"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Seat != 0 || msg.Reason != "no_clear" {
		t.Fatalf("rejected = %+v, want seat 0 reason no_clear", msg)
	}
	if msg.Swap.A != (protocol.Pos{Col: 2, Row: 0}) || msg.Swap.B != (protocol.Pos{Col: 3, Row: 0}) {
		t.Fatalf("swap echo = %+v, want original swap", msg.Swap)
	}
}

func TestFromRoomReshuffle(t *testing.T) {
	typ, payload := roomRoundTrip(t, room.ReshuffleFrame{
		Board: []judge.Tile{2, 2, 2}, Scores: [2]int{1, 1}, Turn: 4,
	})
	if typ != protocol.TypeReshuffle {
		t.Fatalf("type = %s, want reshuffle", typ)
	}
	var msg struct {
		Board  []int  `json:"board"`
		Scores [2]int `json:"scores"`
		Turn   int    `json:"turn"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(msg.Board, []int{2, 2, 2}) || msg.Scores != [2]int{1, 1} || msg.Turn != 4 {
		t.Fatalf("reshuffle = %+v, want board [2 2 2] scores [1 1] turn 4", msg)
	}
}

func TestFromRoomSettlement(t *testing.T) {
	typ, payload := roomRoundTrip(t, room.SettlementFrame{
		Draw: false, Winner: 1, Scores: [2]int{3, 5},
	})
	if typ != protocol.TypeSettlement {
		t.Fatalf("type = %s, want settlement", typ)
	}
	var msg struct {
		Draw   bool   `json:"draw"`
		Winner int    `json:"winner"`
		Scores [2]int `json:"scores"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Draw || msg.Winner != 1 || msg.Scores != [2]int{3, 5} {
		t.Fatalf("settlement = %+v, want winner 1 scores [3 5]", msg)
	}
}

func TestFromRoomReconnectFrames(t *testing.T) {
	// 重连成功：座位 + 全量快照。
	typ, payload := roomRoundTrip(t, room.ReconnectedFrame{
		Seat: 1,
		Snapshot: room.Snapshot{
			Board: []judge.Tile{1, 2}, Scores: [2]int{1, 0}, Turn: 3, Operator: 1,
			Deadline: time.Date(2026, 9, 28, 12, 0, 30, 0, time.UTC),
		},
	})
	if typ != protocol.TypeReconnected {
		t.Fatalf("type = %s, want reconnected", typ)
	}
	var rec protocol.ReconnectedMsg
	if err := json.Unmarshal(payload, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Seat != 1 || rec.Snapshot.Turn != 3 || !slices.Equal(rec.Snapshot.Board, []int{1, 2}) {
		t.Fatalf("reconnected = %+v, want seat 1 turn 3 board [1 2]", rec)
	}

	// 重连被拒：座位 + 原因。
	typ, payload = roomRoundTrip(t, room.ReconnectRejectedFrame{
		Seat: 0, Reason: room.ReconnectBadToken,
	})
	if typ != protocol.TypeReconnectRejected {
		t.Fatalf("type = %s, want reconnect_rejected", typ)
	}
	var rej protocol.ReconnectRejectedMsg
	if err := json.Unmarshal(payload, &rej); err != nil {
		t.Fatal(err)
	}
	if rej.Seat != 0 || rej.Reason != "bad_token" {
		t.Fatalf("reconnect rejected = %+v, want seat 0 bad_token", rej)
	}
}
