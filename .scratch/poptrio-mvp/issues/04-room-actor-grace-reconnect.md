# 04: 房间 Actor：掉线宽限与重连快照

**What to build:** Room 边缘路径：玩家掉线即启动 60 秒宽限定时器；宽限期内 Reconnect 命令凭对局 token 返回全量快照（Board、双方 Score、当前操作方、回合序号、窗口截止时间戳）恢复对局；掉线者的 Turn 照常计时流逝，无单独跳过惩罚；宽限期届满未归 = 判负 Settlement（对方胜利），Room 释放。进程内测试，无网络。

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] 掉线后宽限期内 Reconnect：全量快照与当时对局状态一致，对局无缝继续
- [ ] 掉线者的 Turn 照常计时流逝
- [ ] 宽限期届满未归：发出判负 Settlement 事件，Room 释放
- [ ] 宽限期时长为配置项
- [ ] go test 全绿
