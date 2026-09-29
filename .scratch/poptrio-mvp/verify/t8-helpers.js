// t8 扩展助手：设计坐标换算、回合等待（只做有效交换）、帧统计。
// 依赖 t7-helpers.js 先行安装（__frames / __pt / __snap / __findSwaps）。
window.__ptd = (wx, wy) => { const c = document.querySelector('#GameCanvas') || document.querySelector('canvas'); if (!c) return { x: 0, y: 0 }; const r = c.getBoundingClientRect(); const scale = r.height / 1280; return { x: Math.round(r.left + r.width / 2 + wx * scale), y: Math.round(r.top + r.height / 2 - wy * scale) }; };
window.__ptc = (col, row) => ({ wx: -304 + col * 68 + 32, wy: -40 + (304 - row * 68 - 32) });
window.__waitTurn8 = () => new Promise((resolve) => { const t0 = Date.now(); const tick = () => { const s = window.__snap(); if (window.__frames.some(f => f.msg && f.msg.type === 'settlement')) { resolve('SETTLED'); return; } if (s && s.operator === window.__mySeat && s.turn !== window.__playedTurn && Date.parse(s.deadline) - Date.now() > 4000) { const sw = window.__findSwaps()[0]; if (!sw) { resolve('NO-SWAP'); return; } const a = window.__ptc(sw.a.col, sw.a.row); const b = window.__ptc(sw.b.col, sw.b.row); resolve([s.turn, a.wx, a.wy, b.wx, b.wy].join(',')); return; } if (Date.now() - t0 > 25000) { resolve('TIMEOUT'); return; } setTimeout(tick, 150); }; tick(); });
window.__lastSettlement = () => { const ss = window.__frames.filter(f => f.msg && f.msg.type === 'settlement'); return ss.length ? JSON.stringify(ss[ss.length - 1].msg.payload) : 'null'; };
window.__lastReconnectedSnap = () => { const rs = window.__frames.filter(f => f.msg && f.msg.type === 'reconnected'); return rs.length ? JSON.stringify(rs[rs.length - 1].msg.payload.snapshot) : 'null'; };
window.__outCount = (t) => window.__frames.filter(f => f.dir === 'out' && f.msg && f.msg.type === t).length;
'installed-t8'
