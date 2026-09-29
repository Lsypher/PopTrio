// 场景舞台搭建：竖屏设计分辨率（720x1280，FIT_HEIGHT——完整高度恒可见，
// 桌面宽屏下内容居中，移动竖屏与桌面浏览器均可玩）+ 程序化 Canvas/Camera。
// 场景 JSON 保持极简，UI 全部由代码搭建（骨架期占位实现）。
import {
  Camera,
  Canvas,
  Color,
  director,
  Layers,
  Node,
  ResolutionPolicy,
  UITransform,
  view,
  Widget,
} from 'cc';

export const DESIGN_WIDTH = 720;
export const DESIGN_HEIGHT = 1280;

/** 确保当前场景存在 2D 舞台（Canvas + 正交 UI 相机），返回 Canvas 节点。 */
export function ensureStage(): Node {
  view.setDesignResolutionSize(DESIGN_WIDTH, DESIGN_HEIGHT, ResolutionPolicy.FIXED_HEIGHT);
  const scene = director.getScene();
  let canvas = scene.getChildByName('Canvas');
  if (canvas) {
    return canvas;
  }
  canvas = new Node('Canvas');
  canvas.layer = Layers.Enum.UI_2D;
  canvas.addComponent(UITransform);
  const canvasComp = canvas.addComponent(Canvas);

  const camNode = new Node('UICamera');
  camNode.layer = Layers.Enum.UI_2D;
  camNode.setPosition(0, 0, 1000);
  const cam = camNode.addComponent(Camera);
  cam.projection = Camera.ProjectionType.ORTHO;
  cam.clearFlags = Camera.ClearFlag.SOLID_COLOR;
  cam.clearColor = new Color(24, 26, 32, 255);
  cam.visibility = Layers.Enum.UI_2D;
  // Canvas 不会自动同步程序化相机的正交高度：设计分辨率定高 1280，
  // 半高 640 即世界可视高度一半；远平面需覆盖 z=1000 的相机距离。
  cam.orthoHeight = DESIGN_HEIGHT / 2;
  cam.far = 2000;
  camNode.parent = canvas;
  canvasComp.cameraComponent = cam;

  const widget = canvas.addComponent(Widget);
  widget.isAlignTop = true;
  widget.isAlignBottom = true;
  widget.isAlignLeft = true;
  widget.isAlignRight = true;
  widget.top = 0;
  widget.bottom = 0;
  widget.left = 0;
  widget.right = 0;
  widget.alignMode = Widget.AlignMode.ALWAYS;

  canvas.parent = scene;
  return canvas;
}
