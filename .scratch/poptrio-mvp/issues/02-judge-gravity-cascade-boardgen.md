# 02: 判定器：重力、连锁、死局与棋盘生成

**What to build:** 判定器纯函数库的第二半：Clear 后 Tile 重力下落、顶部随机补充（6 色）、Cascade 多波结算（一次 Swap 触发的全部连锁波次归单一操作者入参）、Dead Board 检测（全部相邻 Swap 无效）、初始棋盘生成（约束：无现成 Line + 至少存在一步有效 Swap）、Reshuffle 生成。判定器至此完整。

**Blocked by:** 01

**Status:** ready-for-agent

- [x] 消除后下落与补充正确：棋盘无空洞、每列连续、新棋子仅出现在顶部
- [x] 连锁循环收敛：多波 Cascade 全部结算，得分汇总归同一操作者
- [x] Dead Board 检测：存在/不存在有效 Swap 的棋盘判定正确
- [x] 初始棋盘生成满足两约束（统计性测试：多次生成全部满足）
- [x] Reshuffle 生成结果满足两约束（新棋盘可继续对局）
- [x] go test 全绿

## Comments

- 2026-09-28：实现完成，`go test ./...` 全绿（判定器包覆盖率 97.8%，未覆盖仅为不可达 panic 路径）。
  - `cascade.go`：`TileRNG`（注入式随机源，判定器唯一随机入口）、`Board.Settle`（重力沉底保序 + 顶部补充，补充索取顺序：列自左向右、列内自上而下）、`ApplySwap(board, swap, operator, rand)`（一次交换触发全部连锁波次的单一结算入口，`CascadeResult{Operator, Waves, TotalScore, Board}`，得分汇总归 operator 一人；无效/回弹交换返回 ok=false 且不动棋盘）。
  - `judge.go` 追加：`HasValidSwap` / `IsDeadBoard`（只检视交换涉及两格的 O(1) run 检查，等色交换恒无效）。
  - `generate.go`：`GenerateBoard`（拒绝采样满足两约束：无现成连线 + 至少一步有效交换；初始棋盘与 Reshuffle 共用，Reshuffle 结果天然异于死局）。随机性全部经 `TileRNG` 注入，保持纯函数语义。
  - 测试要点：脚本化随机源确定性构造两波连锁（含收敛断言）；bg9 图案与 4x4 mod-3 图案为手工推演死局样本；`HasValidSwap` 以「逐对交换副本 + FindClear」oracle 在随机 line-free 棋盘上做 500 次交叉验证。
