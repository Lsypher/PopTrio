# 0005 - UI 编辑器搭建化（静态骨架进场景/Prefab，代码只留逻辑）

骨架期 UI 全部由代码在运行时构建（LobbyUI/MatchUI/SettlementView 的 makeLabel/makeButton 与 Stage.ensureStage 程序化舞台），后果是编辑器场景为空壳：界面结构不可视，协作方无法在编辑器内查看与调整 UI，每次视觉改动都要改代码重跑验证。因此决定：静态 UI 骨架（Canvas/相机、标题、按钮、HUD、结算面板等固定结构）由编辑器内搭建并持久化为场景或 Prefab；脚本通过 @property 节点引用接线，只保留行为逻辑（事件绑定、状态刷新、动画驱动）；运行时动态内容（Tile/Slot 生成、计分与状态文案）仍由代码填充。

## Considered Options

- **维持代码搭建 + 双路径回退**（编辑器节点缺失时运行时代码重建）：每改一次 UI 要同步维护两份结构，违背可协作初衷。否决。
- **全部 UI 连棋盘格子一起进编辑器**：Tile/Slot 是运行时按服务端棋盘状态动态生成的，进编辑器只会留下空容器，徒增场景复杂度。否决。

## Consequences

- 场景/Prefab JSON 成为 UI 结构的唯一事实源，视觉调整在编辑器完成并可直接 git 审查。
- Graphics 代码绘制对静态元素失效（编辑器中不可见、场景不序列化绘制内容），按钮/遮罩改用 Sprite + 占位纯色（assets/ui/White.png），美术资源到位后只换贴图不动结构。
- 设计分辨率（720x1280 FIXED_HEIGHT）从运行时 ensureStage 迁到项目设置。
- Stage.ts 程序化舞台职责终结并删除；BoardView 的动态棋盘生成保留代码实现。
- Cocos 3.8 压缩 Prefab 实例的内部组件无法被场景 JSON 直接引用（序列化写 null），外部组件只能引用实例根节点后运行时 getComponent（见 MatchUI.settlementRoot）。
