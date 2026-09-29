# 08: 结算画面、重连恢复与判胜通知

**What to build:** 对局收尾与恢复：终局 Settlement 画面（胜/负/Draw + 双方终分）、返回大厅与再战入口（重新进入 Queue）、客户端断线后凭 token Reconnect 恢复（拉全量快照、恢复倒计时与轮次、继续对战）、对手掉线 60 秒未归时收到判胜通知并进入结算画面。验证：双窗口完整流程 + 断线恢复演练。

**Blocked by:** 07、04

**Status:** ready-for-human

- [x] TypeScript typecheck 通过
- [ ] 终局画面正确显示胜/负/Draw 与双方终分，可返回大厅或再战
- [ ] 对局中断网后重连：宽限期内恢复成功，棋盘/得分/回合/倒计时与断线前一致，可继续操作
- [ ] 对手超宽限期未归：收到判胜 Settlement 通知并进入结算画面
- [ ] 全流程手动验证通过（双浏览器窗口 + 断线恢复）

## Comments

- 2026-09-29（第二次会话）：TRAE 浏览器自动化重试断线演练，**部分验证通过**后环境持续故障（webview 不可见导致 rAF 暂停、合成点击不生效、截图失败），应用户要求中止自动化，剩余项留人工：
  - ✅ **断线自动重连成功**：对局中（自己回合第 9 回合中途）经 `__poptrioNet.close()` 强制断线，客户端立即自动重连，**约 154ms** 内收到 `reconnected` 帧（首次尝试即成功，未进入重试循环）。
  - ✅ **状态恢复一致**：reconnected 快照与掉线前完全一致——turn=9、operator、scores=[6,0]、**deadline 逐字相等**（证明 TurnClock 中途快照不重估偏移、倒计时未重置）、全量棋盘已捕获。
  - ⬜ 重连后「继续落子」未在真实对局中走通（自动化环境问题，非代码问题）；建议人工双窗口补验：一局中断网（DevTools Offline 或 `__poptrioNet.close()`）→ 自动恢复 → 随手走一步确认可继续操作。
- 2026-09-29：客户端实现完成（服务端重连/宽限/结算语义 issue 04/05 已具备，本次零服务端改动）。`npx tsc -p client/tsconfig.json --noEmit` 通过；`go test ./...` 全绿（回归确认）。
  - `match/SettlementView.ts`（新增）：全屏遮罩 + 胜/负/平局标题（本方座位视角，座位未知退化为中立）+「你 X : Y 对手」双方终分 + 再战/返回大厅双按钮；BlockInputEvents 吞触摸，结算后不可再操作棋盘。
  - `match/MatchUI.ts`：① 断线自动重连——对局中断线（`netClose` 且未终局、会话活跃、有 token）即进入重连循环：connect → `encodeReconnect(token)`，2s 间隔重试、5s 帧响应兜底、70s（宽限 60s + 余量）放弃；`reconnected` 停循环并硬同步，`reconnect_rejected`/放弃路径显示「返回大厅」出口按钮；重连中的 error 帧按可重试处理（旧连接未清理的 bad_token 竞态窗口）。② `resync(snapshot, freshWindow)` 区分窗口起始/中途快照。③ `renderSettlement` 接入 SettlementView。④ 再战=置 `matchSession.autoQueue` 后切大厅；返回大厅=清标志后切大厅。
  - `match/TurnClock.ts`：拆分 `syncWindowStart`（估时钟偏移、跨快照 min 收敛——仅窗口起始快照可信）与 `resnap`（窗口中途快照只刷 deadline、沿用已收敛偏移）。修复原实现把「重连中途快照」当窗口起点重估偏移、剩余时间被放大到整窗的问题（倒计时恢复正确性的关键）。
  - `core/MatchSession.ts`：`reconnected` 是房间广播（对手也会收到），`mySeat` 仅在未知时兜底赋值，修复对手收到该帧后座位被覆盖的问题；新增 `autoQueue` 再战标志。
  - `lobby/LobbyUI.ts`：onLoad 检测 `autoQueue` 自动进入排队（再战入口）。
  - `net/NetClient.ts`：`?debug=1` 时暴露 `window.__poptrioNet`（断线演练注入点，正常游玩零差异）。
  - **预览已确认**：资产入库（SettlementView.ts.meta 生成）、预览编译新代码（`__poptrioNet` 桥在页面可用）、大厅正常渲染。
  - **未完成**：双窗口浏览器演练（重连恢复、判胜结算画面、再战/返回大厅实拍）因自动化环境持续故障未能完成——agent-browser 守护进程反复握手失败（"started concurrently with different daemon configuration"）、沙箱拦截其状态目录、CLI 传大段 JS 挂起；多次清理重试后输出已不可信，故主动中止。上述四项复选框留给人工/下次会话验证；驱动脚本与助手已就绪：`verify/t8-helpers.js`（依赖 t7-helpers）+ `verify/t8-drive1.ps1`（base64 传参版，含强制断线 `__poptrioNet.close()` 与对手拒连注入）。
