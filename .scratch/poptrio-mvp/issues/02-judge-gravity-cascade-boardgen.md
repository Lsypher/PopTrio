# 02: 判定器：重力、连锁、死局与棋盘生成

**What to build:** 判定器纯函数库的第二半：Clear 后 Tile 重力下落、顶部随机补充（6 色）、Cascade 多波结算（一次 Swap 触发的全部连锁波次归单一操作者入参）、Dead Board 检测（全部相邻 Swap 无效）、初始棋盘生成（约束：无现成 Line + 至少存在一步有效 Swap）、Reshuffle 生成。判定器至此完整。

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] 消除后下落与补充正确：棋盘无空洞、每列连续、新棋子仅出现在顶部
- [ ] 连锁循环收敛：多波 Cascade 全部结算，得分汇总归同一操作者
- [ ] Dead Board 检测：存在/不存在有效 Swap 的棋盘判定正确
- [ ] 初始棋盘生成满足两约束（统计性测试：多次生成全部满足）
- [ ] Reshuffle 生成结果满足两约束（新棋盘可继续对局）
- [ ] go test 全绿
