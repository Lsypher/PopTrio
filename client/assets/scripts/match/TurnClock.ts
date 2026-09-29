// 回合窗口倒计时：以服务端快照中的 deadline 时间戳本地插值。
// 客户端与服务端时钟存在偏移，且快照到达有网络延迟，两种快照必须区别对待：
// - 窗口起始快照（match_started / turn_started）：「到达时刻 ≈ 窗口刚开始」，
//   据此估算 offset = (deadline - WINDOW) - Date.now()。到达延迟（RTT、缓冲）
//   使估计偏大（剩余偏长），故跨快照取最小值收敛到真实时钟差。
// - 窗口中途快照（重连恢复 reconnected）：真实剩余未知——若照搬上式重估，
//   估计会偏小（被 min 收敛选中），剩余时间被错误放大到整窗。故只刷新
//   deadline，沿用已收敛的偏移。
// 精度受 RTT/2 影响且只偏保守（倒计时略偏短/准），MVP 可接受。
const WINDOW_MS = 10_000; // 与服务端 room.DefaultConfig().TurnSeconds 对齐

export class TurnClock {
  private deadlineMs = 0;
  private offsetMs = 0;

  /** 以窗口起始快照的 deadline（RFC3339）校准偏移并刷新截止。 */
  syncWindowStart(deadlineIso: string) {
    const deadline = Date.parse(deadlineIso);
    if (!Number.isFinite(deadline)) {
      return;
    }
    const candidate = deadline - WINDOW_MS - Date.now();
    this.offsetMs = this.deadlineMs ? Math.min(this.offsetMs, candidate) : candidate;
    this.deadlineMs = deadline;
  }

  /** 以窗口中途快照（重连恢复）的 deadline 刷新截止，偏移沿用既有收敛值。 */
  resnap(deadlineIso: string) {
    if (!this.deadlineMs) {
      return; // 尚无窗口起始锚点：无法可信插值，保持未同步
    }
    const deadline = Date.parse(deadlineIso);
    if (!Number.isFinite(deadline)) {
      return;
    }
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
