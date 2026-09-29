# 06: Cocos 客户端骨架与匹配入口

**What to build:** Cocos Creator 3.8.8 + TypeScript 客户端骨架：工程脚手架与移动竖屏适配（桌面可玩）、TS 协议客户端模块（与 WS 协议消息一一对齐）、大厅场景（"开始匹配 / 取消"）、排队等待与超时提示、匹配成功切换对局场景并接收初始全量快照（可先用占位视图呈现棋盘数据）、构建产物可静态托管。

**Blocked by:** 05

**Status:** ready-for-human

- [x] TypeScript typecheck 通过
- [x] 浏览器打开：点开始匹配 → 显示排队中；取消 → 回到大厅
- [x] 排队超时显示"暂无对手"提示，可重新排队
- [x] 双浏览器窗口同时排队 → 双双进入对局场景，各自收到一致的初始快照
- [x] 构建产物静态托管可正常访问
- [x] 竖屏布局正确，桌面浏览器可玩

## Comments

- 2026-09-29：客户端实现与浏览器验收完成。typecheck：`tsc -p client/tsconfig.json --noEmit` 全绿（修复 `Stage.ts` `cam.farClip`→`cam.far`，cc.Camera 组件无 farClip）；服务端 `go test ./...` 全绿。
  - 修复服务端真实缺陷：`hub.go` serveWS 原用 `websocket.Accept(w, r, nil)`，coder/websocket v1.8.15 起默认拒绝跨源握手（403）——静态托管页(8600)与 WS(8080) 必然跨源，任何浏览器都连不上；已改 `InsecureSkipVerify: true`（本机回环 MVP 可接受）。issue 05 注释中"WS 未校验 Origin"结论实为此缺陷的误记。
  - 浏览器验收（agent-browser，截图存 `.scratch/poptrio-mvp/verify/`）：大厅初始态 → 排队中（"排队中…等待对手/取消匹配"）→ 取消回大厅（上一会话）；排队超时（8081 实例 6s 超时）：静置后显示"暂无对手，请重新排队"、按钮回到"开始匹配"，可再次排队（t1-*）；双浏览器窗口同时排队 → 双双进入对局场景，同一时刻双窗均为"0 : 0 / 回合 10 · 操作方 P0"、9x9 六色棋盘逐格一致（t2-3/t2-4）→ 打满 12 回合后双窗显示"终局 · 平局"（t3-*）。
  - 对局场景渲染确认：棋盘占位色块（9x9、六色板）、比分、回合标签均正常；MSG_TURN_STARTED（回合 1→12 双窗同步推进）与 MSG_SETTLEMENT（终局·平局）分支经真实对局冒烟通过。MSG_RESHUFFLE / MSG_SWAP_REJECTED 浏览器侧难以人为构造（死局/非法交换由服务端单测覆盖），其棋盘重绘路径与 TURN_STARTED 共用同一 drawBoard，不阻塞。
  - 说明：先手由服务端随机掷出（room.go `coin()`），"回合 1 · 操作方 P0"并非必然；Room.emit 向双座位广播同一帧，快照一致性由服务端语义保证。
  - 遗留人工项（不阻塞代码验收）：在 Cocos 编辑器重新构建 web-mobile，覆盖 `client/build/web-mobile/assets/main/index.js` 的手工补丁（`farClip=2000`→`far=2000`，否则旧产物远裁剪面 1000 会裁掉 z=1000 处 UI 平面致黑屏）。编辑器 MCP builder 消息在本环境不可用，需人工点构建面板。
  - 2026-09-29 补充：编辑器重建完成，产物为 `client/build/web-mobile-001/`（压缩构建，`p.far=2e3` 已由源头生效）。8600 静态托管已切至新目录并复验：大厅 → 双窗排队配对 → 对局场景棋盘/比分/回合标签渲染正常（t4-*，回合 1 · P0 先手、双窗棋盘逐格一致）。旧目录 `client/build/web-mobile/`（含手工补丁）保留，可自行清理。至此 issue 06 全部验收项含构建产物收尾完成。
