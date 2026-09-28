# 01: 判定器：连线检测与线性计分

**What to build:** Go 服务端判定器（judge）纯函数库的第一半：Board/Tile 数据模型（9x9、6 色，尺寸与颜色数为注入配置）、相邻 Swap 有效性判定、横竖 Line 检测（≥3 同色）、L/T 交叉合并为一次 Clear 并按去重格数计分、线性 Score（N 枚 = N 分，全场景无加成）。table-driven 测试全绿即交付。

**Blocked by:** None（可立即开工）

**Status:** ready-for-agent

- [x] 判定器为纯函数：无副作用、无 IO、无全局状态；棋盘尺寸与颜色数为注入配置
- [x] 相邻 Swap 有效；非相邻 Swap 无效
- [x] 3/4/5 枚横竖连线全部检出；2 枚不构成 Line；斜线排列永不消除
- [x] L 形/T 形交叉连线合并为一次 Clear，共享格不重复计分
- [x] 一次 Clear N 枚 Tile 恰好得 N 分
- [x] go test 全绿

## Comments

- 2026-09-28: 已实现。仓库布局调整为 `server/`（Go module `poptrio/server`）+ `client/`（Cocos，issue 06 时创建）；代码在 `server/internal/judge/`。
- 交付面：`Config` 注入尺寸/颜色数（`DefaultConfig`=9x9/6 色）、`Board`/`Tile`/`Pos` 模型、`ValidSwap`（正交邻接 + 越界拒绝）、`FindLines`（横竖 ≥3 同色最大游程，斜线无效）、`FindClear`（全部连线合并为单次 Clear，L/T 共享格去重，row-major 排序）、`Clear.Score`（N 枚 = N 分）。
- 语义决定：一次棋盘上互不相交的多组连线也合并进同一个 Clear——线性计分下总分不变，且与 spec"一次 Swap 在事件循环内瞬时完成全部结算"的单次结算模型一致。
- 验证：47 个 table-driven 子用例全绿（gofmt / go vet / go test 通过）；code-review 双轴（Standards + Spec）无阻塞项。重力/连锁/死局/棋盘生成/重排按边界归 issue 02。
- 遗留建议：将"判定器/Judge"补入 CONTEXT.md 词汇表（domain-modeling 时）。
