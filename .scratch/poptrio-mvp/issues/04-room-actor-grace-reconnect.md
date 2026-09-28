# 04: 房间 Actor：掉线宽限与重连快照

**What to build:** Room 边缘路径：玩家掉线即启动 60 秒宽限定时器；宽限期内 Reconnect 命令凭对局 token 返回全量快照（Board、双方 Score、当前操作方、回合序号、窗口截止时间戳）恢复对局；掉线者的 Turn 照常计时流逝，无单独跳过惩罚；宽限期届满未归 = 判负 Settlement（对方胜利），Room 释放。进程内测试，无网络。

**Blocked by:** 03

**Status:** ready-for-agent

- [x] 掉线后宽限期内 Reconnect：全量快照与当时对局状态一致，对局无缝继续
- [x] 掉线者的 Turn 照常计时流逝
- [x] 宽限期届满未归：发出判负 Settlement 事件，Room 释放
- [x] 宽限期时长为配置项
- [x] go test 全绿

## Comments

- 2026-09-28：实现完成，`go vet` + `go test ./...` + `go test -race ./...` 全绿（judge / room；本机并行会话正在写 matchmaker 包，其半成品 vet 报错不属本 issue 范围）。
  - `server/internal/room/room.go`：命令集扩展 `disconnectCmd` / `reconnectCmd` / `graceExpiryCmd`，与 Swap、回合截止同入单一 channel 串行处理（ADR-0004），新增公开方法 `Disconnect(seat)` / `Reconnect(seat, token)` / `SubmitGraceExpiry(seat)`。宽限定时器与回合计时器同构：真实定时器（掉线即启动 `cfg.GraceSeconds`、重连后取消）属协议层 issue 05，进程内测试注入届满命令确定性驱动；`GraceSeconds` 配置项默认 60，`New` 校验为正。重连 = token 校验（`New` 增每座位 tokens 参数，由匹配层签发、私通道下发、绝不广播）通过即清掉线标记并广播 `ReconnectedFrame` 内嵌全量 `Snapshot`（棋盘、双方得分、当前操作方、回合序号、窗口截止——ADR-0003「重连 = 拉一次快照」），对局状态零改动、无缝续局；已在线座位重复重连幂等（等价快照拉取，重试安全）。宽限届满仅对当前掉线座位生效——已重连座位的迟到届满命令忽略；判负 Settlement（对方胜、当前得分即终局得分）复用 `settle`，`Run` 返回即 Room 释放。掉线本身不改任何对局状态：掉线者回合照常开窗流逝、无跳过惩罚。
  - code-review（Standards + Spec 双轴）后修复/补充：
    - `validSeat` 守卫三个新命令入口：越界座位命令直接忽略——`handleSwap` 因先比 operator 天然免疫越界索引，新命令的 `disconnected[seat]`/`tokens[seat]` 会 panic 波及同房玩家，违背 ADR-0001（客户端可被篡改）与 issue 05「进程不崩溃」。
    - `settle(winner int)` 以 `winner == -1` 派生 Draw，删除布尔标志参数。
    - 补测：重连成功后迟到届满命令被忽略（对局照常打满 Draw，不判负）；越界座位命令无副作用（不 panic、零额外帧）。
  - 测试（沿用 issue 03 主接缝：进程内直投命令、断言广播帧序列）：非操作方掉线后重连快照与前一 `turn_started` 快照 `reflect.DeepEqual` 逐字段一致且对局打满正常结算；操作方掉线其窗口照常截止易手、对方得分后重连快照正确（无冻结、无跳过惩罚）；宽限判负两路径（开局零分 / 对方已得分）帧序恰为 MatchStarted(+结果帧) + Settlement，`runRoom` 阻塞等待 `Run` 返回（5s 超时即失败）即「Room 释放」的严格断言；坏 token 回执 `reconnect_rejected` 且掉线状态保留（届满仍判负）；`GraceSeconds` 默认 60 断言 + 自定义值对局冒烟。
