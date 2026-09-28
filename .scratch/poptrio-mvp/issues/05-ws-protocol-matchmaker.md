# 05: WS 协议层与匹配器

**What to build:** 单 Go 进程对外服务：JSON over WebSocket 协议（消息带版本字段，编解码集中在协议模块）、双玩家连接接入 Room、Matchmaking Queue（随机配对、2 分钟排队超时自动退出并通知、手动取消）、HTTP 健康检查、进程可启动运行。验证：两个脚本化 WS 客户端从排队到终局走完整局。

**Blocked by:** 03（与 04 并行，互不阻塞）

**Status:** ready-for-agent

- [ ] 两个脚本 WS 客户端可排队、配对、进入对局、完整打满并收到 Settlement 帧
- [ ] 排队 2 分钟超时：客户端收到超时事件并退出队列；手动取消立即生效
- [ ] 消息携带版本字段；编解码集中在独立协议模块，可整体替换
- [ ] 非法消息、未知消息类型返回明确错误帧，进程不崩溃
- [ ] HTTP 健康检查可用
- [ ] 排队超时时长为配置项
- [ ] go test 全绿（含脚本客户端对局冒烟测试）

## Comments

- 2026-09-28：自 issue 03 交接——`room.Room` 在 Settlement 后 `Run` 返回，`cmds` channel 不再被消费；此后 `SubmitSwap/SubmitDeadline` 仅能再入队 64 笔缓冲，超出即阻塞投递方。WS 泵接线时须在终局后停止向房间投递（或经 `room` 包后续提供的完成信号感知终局），避免玩家在结算瞬间连发消息卡死读泵。
