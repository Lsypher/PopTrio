# 01: 判定器：连线检测与线性计分

**What to build:** Go 服务端判定器（judge）纯函数库的第一半：Board/Tile 数据模型（9x9、6 色，尺寸与颜色数为注入配置）、相邻 Swap 有效性判定、横竖 Line 检测（≥3 同色）、L/T 交叉合并为一次 Clear 并按去重格数计分、线性 Score（N 枚 = N 分，全场景无加成）。table-driven 测试全绿即交付。

**Blocked by:** None（可立即开工）

**Status:** ready-for-agent

- [ ] 判定器为纯函数：无副作用、无 IO、无全局状态；棋盘尺寸与颜色数为注入配置
- [ ] 相邻 Swap 有效；非相邻 Swap 无效
- [ ] 3/4/5 枚横竖连线全部检出；2 枚不构成 Line；斜线排列永不消除
- [ ] L 形/T 形交叉连线合并为一次 Clear，共享格不重复计分
- [ ] 一次 Clear N 枚 Tile 恰好得 N 分
- [ ] go test 全绿
