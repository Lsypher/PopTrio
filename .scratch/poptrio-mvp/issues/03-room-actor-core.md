# 03: 房间 Actor：核心对局状态机（进程内）

**What to build:** Room 单 goroutine 事件循环（ADR-0004）：所有命令经 channel 串行处理，事件帧向双玩家输出。合成玩家命令可打完整局：12 个 Turn 交替流转、每 Turn 10 秒窗口、先手随机、Swap 按到达顺序调用判定器、无效 Swap 拒绝且不改变状态、窗口截止后迟到请求一律拒绝、棋盘落定后 Dead Board 检测与 Reshuffle 广播、Attribution 记账累计、12 回合打满后 Settlement（胜/负/Draw）。无网络 IO。

**Blocked by:** 02

**Status:** ready-for-agent

- [x] Room 内绝对串行：单一 goroutine 独占写状态，无锁
- [x] 合成命令驱动完整一局：事件帧序列符合协议语义（快照 + 结果帧）
- [x] 窗口截止与 Swap 几乎同时到达：无竞态、结果确定（截止后必拒）
- [x] 无效 Swap 不改变棋盘状态、不结束 Turn
- [x] 注入 Dead Board 触发 Reshuffle 事件，双方分数与剩余回合不变
- [x] 终局 Settlement 正确：胜/负/Draw 三路径各有测试
- [x] 回合秒数、总回合数为配置项
- [x] go test 全绿

## Comments

- 2026-09-28：实现完成，`go vet ./...` + `go test ./...` 全绿（judge 0.63s / room 1.83s）。
  - `server/internal/room/room.go`：`Room` 事件循环——全部输入（`SubmitSwap` / `SubmitDeadline`）汇入单一 buffered channel 串行处理，对局状态仅事件循环 goroutine 可写，无锁；`Run` 发出 MatchStarted 后循环至 Settlement 即返回。窗口截止即命令：迟到截止（非当前回合）忽略，截止先入队则后续 Swap 按新回合归属判定（易手必拒）——竞态由 channel 到达顺序唯一决定，结果确定。
  - `server/internal/room/frames.go`：事件帧词汇表（ADR-0003 快照帧 + 结果帧）——`match_started`（先手 + 回合 1 快照）/ `turn_started`（全量快照：棋盘、双方得分、回合序号、操作方、窗口截止时间戳）/ `swap_result`（每连锁波次的消除集合、得分与落定后棋盘 + 最终棋盘 + 累计得分 + 归属方；下落移动与补充棋子由相邻波棋盘差分重放）/ `swap_rejected`（`not_operator` / `invalid_swap` / `no_clear`）/ `reshuffle`（分数与回合不变）/ `settlement`（胜/负/Draw + 双方得分）。帧向双座位 Sink 广播，双流逐帧一致性有测试断言。
  - 关键语义：无效 Swap 三类拒绝均不改变棋盘、不结束回合；棋盘每次落定后 `IsDeadBoard` 检测，死局即 `GenerateBoard` 重排并广播；Attribution 经 `ApplySwap` 的 operator 入参全额归操作者；`TurnSeconds` / `TotalTurns` / 棋盘配置均来自 `room.Config`。
  - 测试（spec 唯一主接缝——进程内直投命令、断言广播帧序列，无网络 IO）：脚本化 `TileRNG` + 固定时钟全确定性驱动；`TestFixtureBoard1Script` 守护棋盘脚本（swapA 一波 3 分、结算后整盘死局、重排复用可行棋盘）；完整一局帧序列逐帧断言；结算胜/负/Draw 三路径表驱动；无效交换三因按序拒绝且棋盘/回合不变；截止↔Swap 双向到达顺序竞态各一测（截止先行必拒 / Swap 先行正常结算）；迟到截止忽略；`TurnSeconds=2, TotalTurns=3` 配置项验证；4 goroutine 并发投递的串行化压测（记账与结算自洽）。
  - 限制：本机（Windows）无 gcc/CGO，`go test -race` 无法运行；并发正确性由 actor 串行结构保证并以 `TestConcurrentSubmitNoRace` 压测兜底，后续在带工具链的 CI 上补跑 `-race`。
- 2026-09-28：code-review（Standards + Spec 双轴）后修复两项：
  - 结果帧按 ADR-0003 补齐「下落移动、补充棋子」语义：`judge.Wave` 增加每波落定后棋盘 `Board`（对 issue 02 产物做加法扩展，`CascadeResult.Board` 仍为最终棋盘；judge 两波连锁测试补波间链式断言），`SwapResultFrame.Waves[i].Board` 随帧下发——前端与上一波棋盘做差分即得下落与补充，零计算反推。
  - `TestConcurrentSubmitNoRace` 原与事件循环共用非并发安全的 `*rand.Rand`，`-race` 下会误报：投递方改用 `math/rand/v2` 顶层函数（并发安全），`TileRNG` 保留事件循环专用 PCG 源。
  - 遗留交接：终局后 `cmds` 不再被消费、投递方可能阻塞——已作为注意事项追加至 issue 05 Comments，由 WS 泵接线时处理。
- 2026-09-28：本机安装 msys2 gcc（16.2.0）后补跑 `-race`：`go test -race ./...` 全绿（judge 4.7s / room 2.7s），并发压测 `TestConcurrentSubmitNoRace` 在 `-race` 下连跑 20 次通过——actor 串行化与双座位 Sink 广播无数据竞态，code-review 修复的投递方随机源并发安全性得到验证。此前「无法跑 `-race`」限制条目作废。
