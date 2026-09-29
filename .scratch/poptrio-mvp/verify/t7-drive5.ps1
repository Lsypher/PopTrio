# PopTrio issue 07 验收驱动 v5：对当前活对局直接打满（无窗口操作）
$ErrorActionPreference = 'Continue'
$dir = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify'
$log = Join-Path $dir 't7-run5.log'
function Log($m) { Add-Content -Path $log -Value ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss.fff'), $m) }
Set-Content -Path $log -Value '=== t7 run5 (live match) ==='

function AB($session, $cmd) { agent-browser --session $session @cmd }
function EvalStr($session, $js) {
  $r = (agent-browser --session $session eval $js)
  if ($null -eq $r) { return '' }
  return $r.Replace('\"', '"').Trim('"')
}

$install = @'
window.__pt = (col, row) => { const c = document.querySelector('#GameCanvas') || document.querySelector('canvas'); const r = c.getBoundingClientRect(); const scale = r.height / 1280; const wx = -304 + col * 68 + 32; const wy = -40 + (304 - row * 68 - 32); return { x: Math.round(r.left + r.width / 2 + wx * scale), y: Math.round(r.top + r.height / 2 - wy * scale) }; };
window.__snap = () => { const ts = window.__frames.filter(f => f.msg && f.msg.type === 'turn_started'); const ms = window.__frames.filter(f => f.msg && f.msg.type === 'match_started'); const last = ts.length ? ts[ts.length - 1].msg.payload.snapshot : (ms.length ? ms[ms.length - 1].msg.payload.snapshot : null); return last; };
window.__findSwaps = () => { const s = window.__snap(); if (!s) return []; const W = 9, B = s.board.slice(); const run = (cell) => { for (const d of [[1,0],[0,1]]) { let n = 1; for (let k = 1; k < 9; k++) { const c = cell.col + d[0]*k, r = cell.row + d[1]*k; if (c>8||r>8||B[r*W+c]!==B[cell.row*W+cell.col]) break; n++; } for (let k = 1; k < 9; k++) { const c = cell.col - d[0]*k, r = cell.row - d[1]*k; if (c<0||r<0||B[r*W+c]!==B[cell.row*W+cell.col]) break; n++; } if (n >= 3) return true; } return false; }; const ok = (a, b) => { const c1 = B[a.row*W+a.col], c2 = B[b.row*W+b.col]; if (c1 === c2) return false; B[a.row*W+a.col] = c2; B[b.row*W+b.col] = c1; const hit = run(a) || run(b); B[a.row*W+a.col] = c1; B[b.row*W+b.col] = c2; return hit; }; const out = []; for (let r = 0; r < W; r++) for (let c = 0; c < W; c++) { if (c+1<W && ok({col:c,row:r},{col:c+1,row:r})) out.push({a:{col:c,row:r},b:{col:c+1,row:r}}); if (r+1<W && ok({col:c,row:r},{col:c,row:r+1})) out.push({a:{col:c,row:r},b:{col:c,row:r+1}}); if (out.length >= 8) return out; } return out; };
window.__samePair = () => { const s = window.__snap(); if (!s) return null; const W = 9, B = s.board; for (let r = 0; r < W; r++) for (let c = 0; c < W; c++) { if (c+1<W && B[r*W+c] === B[r*W+c+1]) return {a:{col:c,row:r},b:{col:c+1,row:r}}; if (r+1<W && B[r*W+c] === B[(r+1)*W+c]) return {a:{col:c,row:r},b:{col:c,row:r+1}}; } return null; };
window.__waitTurn = () => new Promise((resolve) => { const t0 = Date.now(); const tick = () => { const s = window.__snap(); if (window.__frames.some(f => f.msg && f.msg.type === 'settlement')) { resolve('SETTLED'); return; } if (s && s.operator === window.__mySeat && s.turn !== window.__playedTurn && Date.parse(s.deadline) - Date.now() > 4500) { const sw = window.__findSwaps()[0]; const sp = window.__samePair(); if (!sw) { resolve('NO-SWAP'); return; } const p = (c) => window.__pt(c.col, c.row); resolve([s.turn, sp.a.col, sp.a.row, sp.b.col, sp.b.row, sw.a.col, sw.a.row, sw.b.col, sw.b.row].join(',')); return; } if (Date.now() - t0 > 20000) { resolve('TIMEOUT'); return; } setTimeout(tick, 200); }; tick(); });
'ready'
'@

foreach ($s in 'pta', 'ptb') {
  $null = EvalStr $s $install
  $seat = EvalStr $s "String((window.__frames.find(f => f.msg && f.msg.type === 'match_token') || {msg:{payload:{seat:-1}}}).msg.payload.seat)"
  $null = EvalStr $s "window.__mySeat = $seat; window.__playedTurn = 0;"
  Log "helper installed seat=$seat on $s"
}

for ($i = 1; $i -le 16; $i++) {
  Log "cycle $i begin"
  $done = $false
  foreach ($s in 'ptb', 'pta') {
    $raw = EvalStr $s 'window.__waitTurn()'
    Log "cycle $i ${s} waitTurn -> $raw"
    if ($raw -eq 'SETTLED') { Log "cycle $i ${s} settled"; $done = 'settled'; break }
    if ($raw -eq 'TIMEOUT' -or $raw -eq 'NO-SWAP' -or $raw -eq '') { continue }
    $v = $raw.Split(',')
    if ($v.Count -lt 9) { Log "cycle $i ${s} bad-points"; continue }
    Log ("cycle {0} {1} play turn {2}" -f $i, $s, $v[0])
    $null = EvalStr $s "window.__playedTurn = $($v[0])"
    AB $s @('mouse', 'move', "$($v[1])", "$($v[2])") | Out-Null
    AB $s @('mouse', 'down', 'left') | Out-Null
    AB $s @('mouse', 'up', 'left') | Out-Null
    AB $s @('wait', '120') | Out-Null
    AB $s @('mouse', 'move', "$($v[3])", "$($v[4])") | Out-Null
    AB $s @('mouse', 'down', 'left') | Out-Null
    AB $s @('mouse', 'up', 'left') | Out-Null
    AB $s @('wait', '600') | Out-Null
    AB $s @('mouse', 'move', "$($v[5])", "$($v[6])") | Out-Null
    AB $s @('mouse', 'down', 'left') | Out-Null
    AB $s @('mouse', 'up', 'left') | Out-Null
    AB $s @('wait', '120') | Out-Null
    AB $s @('mouse', 'move', "$($v[7])", "$($v[8])") | Out-Null
    AB $s @('mouse', 'down', 'left') | Out-Null
    AB $s @('mouse', 'up', 'left') | Out-Null
    AB $s @('wait', '300') | Out-Null
    AB $s @('screenshot', (Join-Path $dir ("t7-c{0}-{1}-play.png" -f $i, $s))) | Out-Null
    $other = if ($s -eq 'pta') { 'ptb' } else { 'pta' }
    AB $other @('screenshot', (Join-Path $dir ("t7-c{0}-{1}-watch.png" -f $i, $other))) | Out-Null
    $done = $true
    break
  }
  if ($done -eq 'settled') { break }
  if (-not $done) { Log "cycle $i nobody-played" }
}

foreach ($i in 1..90) {
  Start-Sleep -Seconds 1
  $a = EvalStr 'pta' "String(window.__frames.some(f => f.msg && f.msg.type === 'settlement'))"
  $b = EvalStr 'ptb' "String(window.__frames.some(f => f.msg && f.msg.type === 'settlement'))"
  if ($a -eq 'true' -and $b -eq 'true') { Log 'settlement reached'; break }
}
Start-Sleep -Milliseconds 800
AB 'pta' @('screenshot', (Join-Path $dir 't7-final-a.png')) | Out-Null
AB 'ptb' @('screenshot', (Join-Path $dir 't7-final-b.png')) | Out-Null

$sumA = EvalStr 'pta' "JSON.stringify({sent: window.__frames.filter(f => f.dir === 'out' && f.msg && f.msg.type === 'swap').length, results: window.__frames.filter(f => f.msg && f.msg.type === 'swap_result').length, rejected: window.__frames.filter(f => f.msg && f.msg.type === 'swap_rejected').length, waves: window.__frames.filter(f => f.msg && f.msg.type === 'swap_result').reduce((n, f) => n + f.msg.payload.waves.length, 0), lastTurn: (window.__snap() || {}).turn, board: (window.__snap() || {}).board, scores: (window.__snap() || {}).scores})"
$sumB = EvalStr 'ptb' "JSON.stringify({sent: window.__frames.filter(f => f.dir === 'out' && f.msg && f.msg.type === 'swap').length, results: window.__frames.filter(f => f.msg && f.msg.type === 'swap_result').length, rejected: window.__frames.filter(f => f.msg && f.msg.type === 'swap_rejected').length, waves: window.__frames.filter(f => f.msg && f.msg.type === 'swap_result').reduce((n, f) => n + f.msg.payload.waves.length, 0), lastTurn: (window.__snap() || {}).turn, board: (window.__snap() || {}).board, scores: (window.__snap() || {}).scores})"
Log "SUMMARY-A: $sumA"
Log "SUMMARY-B: $sumB"
Log 'done'
