# 05: WS 协议层与匹配器

**What to build:** 单 Go 进程对外服务：JSON over WebSocket 协议（消息带版本字段，编解码集中在协议模块）、双玩家连接接入 Room、Matchmaking Queue（随机配对、2 分钟排队超时自动退出并通知、手动取消）、HTTP 健康检查、进程可启动运行。验证：两个脚本化 WS 客户端从排队到终局走完整局。

**Blocked by:** 03（与 04 并行，互不阻塞）

**Status:** ready-for-human

- [x] 两个脚本 WS 客户端可排队、配对、进入对局、完整打满并收到 Settlement 帧
- [x] 排队 2 分钟超时：客户端收到超时事件并退出队列；手动取消立即生效
- [x] 消息携带版本字段；编解码集中在独立协议模块，可整体替换
- [x] 非法消息、未知消息类型返回明确错误帧，进程不崩溃
- [x] HTTP 健康检查可用
- [x] 排队超时时长为配置项
- [x] go test 全绿（含脚本客户端对局冒烟测试）

## Comments

- 2026-09-28：实现完成，`go vet ./...` + `go test ./...` + `go test -race ./...` 全绿；`go build` 产物前台启动 + `GET /healthz` 200 验证通过。
  - `server/internal/protocol/`：可整体替换的编解码模块——线上信封 `{"v":1,"type":"...","payload":{...}}`（版本字段随每条消息携带），`Parse`（入站 queue_join / queue_cancel / swap / reconnect，坐标越界等游戏规则不在此校验、交由房间 swap_rejected 回绝）与 `Encode`（全部出站消息 + `FromRoom` 把 room.Frame 词汇表一比一映射为线上消息，含 issue 04 新增的 reconnected / reconnect_rejected）。其余包零 JSON、零信封知识。
  - `server/internal/matchmaker/`：全局匹配队列，先到先配（MVP 无段位等匹配条件，"随机配对"即不设条件，见下）；排队超时（`New` 配置项）定时器与手动取消全部锁内摘除，结果经 buffered-1 通道恰好投递一次；配对结果 `Matchup.Start` 以 sync.Once 保证双方并发调用时开局动作全局恰好一次、且完成后另一方才继续——swap 此后必然可安全投递。
  - `server/internal/hub/`：WS 泵接线。每连接 3 goroutine（读泵 / 写泵 / 主循环状态机 idle→queued→inRoom），与房间仅经 channel 通信（ADR-0004）。出站 outbox 非阻塞投递、满则丢帧（慢消费者策略落地 ADR-0004 留白），终局 Settlement 先入队后 close，writer 排空最后一帧后正常关连接。回合计时器 `turnClock` 挂座位 0 的 Sink：快照帧截止时间戳 → `time.AfterFunc` → `SubmitDeadline`，迟到截止由房间忽略；Settlement 即停表。issue 03 交接的终局后投递风险已处理：`finished` 标志（Sink 在 Settlement 置位）守卫 `SubmitSwap`，终局后回错误帧不投递；TOCTOU 残余投递有界无害（入站缓冲 16 + 每回合 1 条截止 ≪ cmds 缓冲 64）。
  - issue 04 交接的协议层定时器接线一并完成：断开即 `room.Disconnect(seat)` + 武装宽限定时器（`GraceSeconds` 配置项），届满 `SubmitGraceExpiry` 判负；新增入站 `reconnect` 消息凭 token 换绑——hub 持 token→座位绑定表，房间 Sink 固定指向绑定而非具体连接（换绑零改动），重连成功停宽限定时器并向房间广播全量快照；终局即摘除绑定（token 作废，事后重连回 bad_token）。token 由 hub 签发（crypto/rand 128bit），`match_token` 帧经各自连接私发、绝不广播。
  - `server/cmd/poptrio/main.go`：进程入口，`GET /healthz`（200 ok）+ `GET /ws`；配置项（env，缺省即 MVP）：`POPTRIO_ADDR` / `POPTRIO_QUEUE_TIMEOUT_SECONDS`（默认 120）/ `POPTRIO_TURN_SECONDS` / `POPTRIO_TOTAL_TURNS` / `POPTRIO_GRACE_SECONDS`。
  - 测试（冒烟主接缝 = httptest 真实 HTTP+WS 服务器 + 脚本 WS 客户端，coder/websocket v1.8.15）：`TestScriptedClientsFullMatch` 双客户端排队→配对（match_token 先于 match_started）→ 2 回合 x 1 秒小配置完整打满（双方各一次白盒查找的有效交换，快照逐帧一致断言）→ Settlement 一致 → 服务端关连接；排队超时 / 手动取消后可重新排队 / 非法消息九连击（坏 JSON、坏版本、未知类型、坏载荷、状态外消息）后连接仍可用 / 掉线宽限判负 / 重连恢复全流程（占用时拒绝→掉线→换绑→对局照常打满）。matchmaker 单测含超时-取消竞争恰好一次投递（50 轮）与 `Matchup.Start` 并发语义。
  - code-review（Standards + Spec 双轴）后修复：`Pairing` 命名违反 CONTEXT.md（_Avoid_: pairing）→ 改 `Matchup`；`go mod tidy` 修正 indirect 标注；配对瞬间对手断线改为存活方原地重新排队（原方案重载 queue_timeout 语义，客户端无感、超时重新起算）；冒烟测试 swap 由条件执行改为确定性（房间不变量：回合开始快照必有可行交换）；修复 endMatch 过早清 session 引用导致座位 1 收不到 Settlement 的缺陷。
  - 已知边界（记录不阻塞）：「重连后取消」与终局瞬间的换绑竞争窗口极小，重连方最多静默等待、由客户端重试超时兜底（issue 08 客户端流程处理）；WS 未校验 Origin（CSWSH 面）与 HTTP 无 ReadHeaderTimeout/优雅退出，MVP 可接受；本机 -race 已跑通（msys2 gcc）。
