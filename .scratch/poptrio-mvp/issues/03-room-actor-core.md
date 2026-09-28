# 03: 房间 Actor：核心对局状态机（进程内）

**What to build:** Room 单 goroutine 事件循环（ADR-0004）：所有命令经 channel 串行处理，事件帧向双玩家输出。合成玩家命令可打完整局：12 个 Turn 交替流转、每 Turn 10 秒窗口、先手随机、Swap 按到达顺序调用判定器、无效 Swap 拒绝且不改变状态、窗口截止后迟到请求一律拒绝、棋盘落定后 Dead Board 检测与 Reshuffle 广播、Attribution 记账累计、12 回合打满后 Settlement（胜/负/Draw）。无网络 IO。

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] Room 内绝对串行：单一 goroutine 独占写状态，无锁
- [ ] 合成命令驱动完整一局：事件帧序列符合协议语义（快照 + 结果帧）
- [ ] 窗口截止与 Swap 几乎同时到达：无竞态、结果确定（截止后必拒）
- [ ] 无效 Swap 不改变棋盘状态、不结束 Turn
- [ ] 注入 Dead Board 触发 Reshuffle 事件，双方分数与剩余回合不变
- [ ] 终局 Settlement 正确：胜/负/Draw 三路径各有测试
- [ ] 回合秒数、总回合数为配置项
- [ ] go test 全绿
