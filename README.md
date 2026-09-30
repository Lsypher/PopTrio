# PopTrio

H5 三消 1v1 实时对战游戏。两名玩家共享一张 9x9 棋盘，轮流交换消除得分，全部规则由服务端判定。客户端用 Cocos Creator 3.8.8，服务端用 Go + WebSocket。

## 玩法规则

- 棋盘为 9x9 网格，6 种颜色的水果棋子，双方共享同一张棋盘。
- 玩家唯一的操作是交换相邻两枚棋子。交换后形成 3 枚及以上同色横竖连线即消除，消除后棋子下落、顶部补充，可能触发连锁消除。
- 每回合 10 秒操作窗口，轮到的玩家在窗口内可以任意多次交换，无效交换只回弹、不消耗回合。对方在窗口内不能操作。
- 一局共 12 回合（双方各 6 个），整局约 120 秒。
- 线性计分：一次消除 N 枚棋子得 N 分，无任何加成系数；L、T 形按去重后的格子数计。
- 得分归属：一次消除（含连锁）由谁的交换触发，分数就记给谁。
- 棋盘上不存在任何有效交换时判为死局，服务端全盘重排，双方得分与剩余回合不变。
- 回合结束后双方同分即平局，不加时。

## 快速开始

环境要求：Go 1.26+、Cocos Creator 3.8.8。

1. 启动服务端，默认监听 `:8080`：

   ```bash
   cd server
   go run ./cmd/poptrio
   ```

   访问 `http://localhost:8080/healthz` 确认服务已就绪。可选环境变量：`POPTRIO_ADDR`（监听地址）、`POPTRIO_TURN_SECONDS`（回合秒数）、`POPTRIO_TOTAL_TURNS`（总回合数）、`POPTRIO_QUEUE_TIMEOUT_SECONDS`（匹配超时）、`POPTRIO_GRACE_SECONDS`（掉线宽限）。

2. 用 Cocos Creator 3.8.8 打开 `client/` 目录，进入预览。客户端默认连接 `ws://127.0.0.1:8080/ws`；在预览页地址后加 `?server=ws://<主机IP>:8080/ws` 可连接局域网内其他机器上启动的服务端（地址配置见 `client/assets/scripts/net/NetClient.ts`）。

3. 再开一个浏览器窗口进入预览，即可匹配成局开始对战。

运行服务端测试：

```bash
cd server
go test ./...
```

## 架构一瞥

- 服务端权威：棋盘状态、消除判定、计分、回合计时、胜负的唯一权威源在服务端，客户端只提交交换意图并渲染结果。见 [ADR 0001](docs/adr/0001-server-authoritative.md)。
- 竞速回合模型：回合是 10 秒操作窗口而非单次行动，窗口内不限交换次数。见 [ADR 0002](docs/adr/0002-rush-turn-model.md)。
- 混合协议：回合开始与重连时下发全量快照，回合内下发增量事件帧，JSON over WebSocket；客户端可做乐观表现，与服务端结果冲突时回滚。见 [ADR 0003](docs/adr/0003-hybrid-protocol-and-optimistic-ui.md)。
- 房间 Actor 模型：每个房间一个 goroutine 事件循环串行处理消息，判定器是无副作用的纯函数，便于测试。见 [ADR 0004](docs/adr/0004-room-actor-model.md)。

服务端代码在 `server/`，入口 `cmd/poptrio/main.go`，内部包按 judge（判定）、room（房间状态机）、hub（连接与匹配）、matchmaker（配对队列）、protocol（消息定义）划分。

## 当前状态

MVP 已完成：匹配、12 回合完整对局、断线重连、结算，服务端 `go test ./...` 全部通过。尚未实现奖励发放（MVP 阶段明确不做）与天梯排行等后续功能。

## 文档导读

- [CONTEXT.md](CONTEXT.md)：领域术语表，Match、Turn、Cascade 等词在这里有唯一定义。
- [docs/adr/](docs/adr/)：架构决策记录及其取舍原因。
- [.scratch/poptrio-mvp/](.scratch/poptrio-mvp/)：MVP 规格说明与 issue 清单。
- [AGENTS.md](AGENTS.md)：agent 协作约定（issue 流程、术语维护、MCP/CLI 工具）。
