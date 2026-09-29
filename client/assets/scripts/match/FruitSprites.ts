// 水果素材加载：assets/resources/fruits/fruit-1..6.png（自仓库 图片/ 目录导入）。
// 服务端颜色 ID 0..5 的色板为 红/橙/黄/绿/蓝/紫，素材集中无蓝色水果，
// 蓝位以青绿哈密瓜（fruit-3）就近替代，其余按色相映射。
import { resources, SpriteFrame } from 'cc';

/** 颜色 ID → 素材文件序号（fruit-N）。 */
const FRUIT_FILE_FOR_COLOR = [1, 2, 6, 4, 3, 5];

/** 加载 6 张水果 SpriteFrame，按文件序号排序返回；失败抛错由调用方回退。 */
export function loadFruitFrames(): Promise<SpriteFrame[]> {
  return new Promise((resolve, reject) => {
    resources.loadDir('fruits', SpriteFrame, (err, frames) => {
      if (err) {
        reject(err instanceof Error ? err : new Error(String(err)));
        return;
      }
      const byName = new Map<string, SpriteFrame>();
      for (const f of frames) {
        byName.set(f.name, f);
      }
      const sorted: SpriteFrame[] = [];
      for (let n = 1; n <= 6; n++) {
        const f = byName.get(`fruit-${n}`);
        if (!f) {
          reject(new Error(`missing fruit asset: fruits/fruit-${n}`));
          return;
        }
        sorted.push(f);
      }
      resolve(sorted);
    });
  });
}

/** 颜色 ID → SpriteFrame 下标（FRUIT_FILE_FOR_COLOR[color] - 1）。 */
export function fruitIndexForColor(color: number): number {
  const idx = FRUIT_FILE_FOR_COLOR[color] ?? FRUIT_FILE_FOR_COLOR[0];
  return idx - 1;
}
