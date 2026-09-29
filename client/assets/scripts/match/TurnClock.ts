// 回合窗口倒计时：以服务端快照中的 deadline 时间戳本地插值。
// 客户端与服务端时钟可能存在偏移：每次快照到达时按「到达时刻 ≈ 窗口刚
// 开始」估算时钟偏移 offset = (deadline - WINDOW) - Date.now()。到达延迟
//（RTT、素材加载缓冲）会使该估计偏大（剩余时间偏长），故偏移跨快照取
// 最小值收敛到真实时钟差——顺带修正「窗口中途 reconnected 快照」场景
//（其隐含剩余 = 整窗，偏大估计会被既有偏移压回真实值）。精度受 RTT/2
// 影响且只偏保守（倒计时略偏短/准），MVP 可接受。
const WINDOW_MS = 10_000; // 与服务端 room.DefaultConfig().TurnSeconds 对齐

export class TurnClock {
  private deadlineMs = 0;
  private offsetMs = 0;

  /** 以快照 deadline（RFC3339）重新校准。 */
  sync(deadlineIso: string) {
    const deadline = Date.parse(deadlineIso);
    if (!Number.isFinite(deadline)) {
      return;
    }
    const candidate = deadline - WINDOW_MS - Date.now();
    this.offsetMs = this.deadlineMs ? Math.min(this.offsetMs, candidate) : candidate;
    this.deadlineMs = deadline;
  }

  /** 剩余毫秒，夹取到 [0, WINDOW]。 */
  remainingMs(): number {
    if (!this.deadlineMs) {
      return 0;
    }
    const serverNow = Date.now() + this.offsetMs;
    return Math.max(0, Math.min(WINDOW_MS, this.deadlineMs - serverNow));
  }

  /** 是否已校准过（对局数据到达）。 */
  get synced(): boolean {
    return this.deadlineMs > 0;
  }
}
