# PopTrio issue 08 driver v1: 重连恢复 + 判胜结算 + 再战/返回大厅 + 完整一局
$ErrorActionPreference = 'Continue'
$dir = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify'
$log = Join-Path $dir 't8-run1.log'
function Log($m) { Add-Content -Path $log -Value ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss.fff'), $m) }
Set-Content -Path $log -Value '=== t8 run1 ==='

function AB($session, $cmd) { agent-browser --session $session @cmd }
function EvalStr($session, $js) {
  $r = (agent-browser --session $session eval $js)
  if ($null -eq $r) { return '' }
  return $r.Replace('\"', '"').Trim('"')
}
function ClickDesign($session, $wx, $wy) {
  $p = (EvalStr $session "JSON.stringify(window.__ptd($wx, $wy))") | ConvertFrom-Json
  AB $session @('mouse', 'move', "$($p.x)", "$($p.y)") | Out-Null
  AB $session @('mouse', 'down', 'left') | Out-Null
  AB $session @('mouse', 'up', 'left') | Out-Null
}
function PlayValidSwap($session) {
  $raw = EvalStr $session 'window.__waitTurn8()'
  Log "  ${session} waitTurn8 -> $raw"
  if ($raw -eq 'SETTLED' -or $raw -eq 'TIMEOUT' -or $raw -eq 'NO-SWAP' -or $raw -eq '') { return $raw }
  $v = $raw -split ','
  Log ("  {0} play turn {1}" -f $session, $v[0])
  EvalStr $session "window.__playedTurn = $($v[0])" | Out-Null
  ClickDesign $session $v[1] $v[2]
  Start-Sleep -Milliseconds 400
  ClickDesign $session $v[3] $v[4]
  Start-Sleep -Milliseconds 600
  return 'PLAYED'
}
function WaitFor($session, $js, $seconds, $tag) {
  foreach ($i in 1..([Math]::Ceiling($seconds / 0.7))) {
    Start-Sleep -Milliseconds 700
    $r = EvalStr $session $js
    if ($r -eq 'true') { Log "  ${tag} ok (poll $i)"; return $true }
  }
  Log "  ${tag} FAILED"
  return $false
}

# ---- Stage 0: fresh windows + helpers ----
Log "stage0: fresh windows (script v4, mtime $((Get-Item $PSCommandPath).LastWriteTime.ToString('HH:mm:ss')))"
# 实证：对存活 daemon 执行 close --all 后，daemon 无法再拉起浏览器（open 永不写 target、eval 排队死挂）。
# 改为外部强杀 daemon + 自动化 Chrome，下次 CLI 调用会自动拉起全新 daemon。
Get-CimInstance Win32_Process -Filter "Name='agent-browser-win32-x64.exe'" | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
Get-CimInstance Win32_Process -Filter "Name='chrome.exe'" | Where-Object { $_.CommandLine -like '*agent-browser*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
Start-Sleep -Seconds 3
$init = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify\ws-log-init.js'
# agent-browser open <url> 在 Cocos 预览页上永不返回（等待完成信号死挂），
# 但页面实际几秒内就 readyState=complete。看门狗：后台起 open，
# 会话 target 文件落盘且页面 complete 后强杀 CLI 客户端（守护进程与页面保留）。
# 后台 job 环境可能未定义 APPDATA：优先用环境变量，退回硬编码路径
$abExe = "$env:APPDATA\npm\node_modules\agent-browser\bin\agent-browser-win32-x64.exe"
if (-not $abExe -or -not (Test-Path $abExe)) { $abExe = 'C:\Users\nxm\AppData\Roaming\npm\node_modules\agent-browser\bin\agent-browser-win32-x64.exe' }
Log "  abExe=$abExe (exists=$(Test-Path $abExe))"
function OpenPage($session) {
  $started = Get-Date
  $p = Start-Process -FilePath $abExe -ArgumentList @('--session', $session, 'open', '--init-script', $init, 'http://localhost:7456/?debug=1') -PassThru -WindowStyle Hidden
  if (-not $p) { Log "FATAL: start-process failed for $session"; exit 1 }
  $tgt = Join-Path $env:USERPROFILE ".agent-browser\$session.target"
  $ready = $false
  while (((Get-Date) - $started).TotalSeconds -lt 45) {
    Start-Sleep -Seconds 2
    # 不能在本会话上 eval 探测：daemon 按会话串行化操作，open 未返回时 eval 会永久排队。
    # 只看 target 文件落盘，再多给 6s 加载余量，杀掉 open CLI 后再 eval 验证。
    if ((Test-Path $tgt) -and ((Get-Item $tgt).LastWriteTime -gt $started)) {
      Start-Sleep -Seconds 6
      $ready = $true
      break
    }
  }
  if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force }
  $rs = ''
  if ($ready) {
    # 杀掉 open 客户端后 daemon 需要片刻清理挂起操作，eval 可能短暂失败，重试兜底
    foreach ($i in 1..5) {
      Start-Sleep -Seconds 1
      $rs = EvalStr $session 'String(document.readyState == ''complete'')'
      if ($rs -eq 'true') { break }
    }
  }
  Log "  open $session ready=$ready rs=$rs"
  if (-not $ready -or $rs -ne 'true') { Log 'FATAL: page not ready'; exit 1 }
}
# 助手 JS 以 base64 传输：多行原文经 CLI 引号转换易挂起
$install7 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content (Join-Path $dir 't7-helpers.js') -Raw)))
$install8 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content (Join-Path $dir 't8-helpers.js') -Raw)))
OpenPage 'pta'
OpenPage 'ptb'
Start-Sleep -Seconds 3
foreach ($s in 'pta', 'ptb') {
  Log "  installing helpers on $s"
  $null = EvalStr $s "eval(atob('$install7'))"
  $null = EvalStr $s "eval(atob('$install8'))"
  $bridge = EvalStr $s "String(typeof window.__poptrioNet)"
  Log "  $s bridge=$bridge"
  if ($bridge -ne 'object') { Log 'FATAL: debug bridge missing (preview not rebuilt?)'; exit 1 }
}

# ---- Stage 1: queue both -> match ----
Log 'stage1: queue both'
ClickDesign 'pta' 0 -100
Start-Sleep -Seconds 1
ClickDesign 'ptb' 0 -100
if (-not (WaitFor 'pta' "String(window.__frames.some(f => f.msg && f.msg.type === 'match_started'))" 15 'pta match_started')) { exit 1 }
if (-not (WaitFor 'ptb' "String(window.__frames.some(f => f.msg && f.msg.type === 'match_started'))" 15 'ptb match_started')) { exit 1 }
foreach ($s in 'pta', 'ptb') {
  $seat = EvalStr $s "String((window.__frames.find(f => f.msg && f.msg.type === 'match_token') || {msg:{payload:{seat:-1}}}).msg.payload.seat)"
  $null = EvalStr $s "window.__mySeat = $seat; window.__playedTurn = 0;"
  Log "  $s seat=$seat"
}

# ---- Stage 2: warm-up turns ----
Log 'stage2: warm-up turns'
foreach ($i in 1..2) {
  foreach ($s in 'ptb', 'pta') {
    $r = PlayValidSwap $s
    if ($r -eq 'PLAYED' -or $r -eq 'SETTLED') { break }
  }
}

# ---- Stage 3: A drops -> auto reconnect -> continue ----
Log 'stage3: A force drop'
AB 'pta' @('screenshot', (Join-Path $dir 't8-1-before-drop-a.png')) | Out-Null
$null = EvalStr 'pta' 'window.__poptrioNet.close()'
Start-Sleep -Milliseconds 800
AB 'pta' @('screenshot', (Join-Path $dir 't8-2-dropped-a.png')) | Out-Null
if (-not (WaitFor 'pta' "String(window.__frames.some(f => f.msg && f.msg.type === 'reconnected'))" 20 'pta reconnected')) {
  AB 'pta' @('screenshot', (Join-Path $dir 't8-3-reconnect-fail.png')) | Out-Null
  exit 1
}
$ns = EvalStr 'pta' 'window.__lastReconnectedSnap()'
$bs = EvalStr 'ptb' 'JSON.stringify(window.__snap())'
Log "  A reconnected snap: $ns"
Log "  B live snap:        $bs"
$na = $ns | ConvertFrom-Json
$nb = $bs | ConvertFrom-Json
$same = ($na.turn -eq $nb.turn) -and ($na.operator -eq $nb.operator) -and ($na.deadline -eq $nb.deadline) -and (($na.scores -join ',') -eq ($nb.scores -join ',')) -and (($na.board -join ',') -eq ($nb.board -join ','))
Log "  snapshot equality: $same"
if (-not $same) { AB 'pta' @('screenshot', (Join-Path $dir 't8-3-snap-mismatch-a.png')) | Out-Null; exit 1 }
Start-Sleep -Seconds 1
AB 'pta' @('screenshot', (Join-Path $dir 't8-4-reconnected-a.png')) | Out-Null
Log 'stage3b: A plays after reconnect'
$ownBefore = EvalStr 'pta' "String(window.__frames.filter(f => f.msg && f.msg.type === 'swap_result' && f.msg.payload.operator === window.__mySeat).length)"
foreach ($i in 1..2) {
  $r = PlayValidSwap 'pta'
  if ($r -eq 'PLAYED' -or $r -eq 'SETTLED') { break }
}
$ownAfter = EvalStr 'pta' "String(window.__frames.filter(f => f.msg && f.msg.type === 'swap_result' && f.msg.payload.operator === window.__mySeat).length)"
Log "  A own swap_result before=$ownBefore after=$ownAfter"
if ($ownAfter -le $ownBefore) { Log 'FATAL: A got no own swap_result after reconnect'; exit 1 }
# B 侧座位未被 reconnected 广播污染：B 仍能正常出swap 并收到自己的结果帧
Log 'stage3c: B plays (seat integrity)'
foreach ($i in 1..2) {
  $r = PlayValidSwap 'ptb'
  if ($r -eq 'PLAYED' -or $r -eq 'SETTLED') { break }
}
$null = WaitFor 'ptb' "String(window.__frames.some(f => f.msg && f.msg.type === 'swap_result' && f.msg.payload.operator === window.__mySeat))" 12 'ptb own swap_result'
AB 'pta' @('screenshot', (Join-Path $dir 't8-5-continue-a.png')) | Out-Null
AB 'ptb' @('screenshot', (Join-Path $dir 't8-5-continue-b.png')) | Out-Null

# ---- Stage 4: B drops and stays offline -> A wins by forfeit ----
Log 'stage4: B offline drill'
$bDropAt = Get-Date
$null = EvalStr 'ptb' 'window.__poptrioNet.close(); window.__poptrioNet.connect = () => Promise.reject(new Error(''offline-drill''))'
AB 'ptb' @('screenshot', (Join-Path $dir 't8-6-b-offline.png')) | Out-Null
# A 期间照常打自己的回合，直到收到判胜 settlement（宽限 60s）
$settled = $false
foreach ($i in 1..14) {
  $r = PlayValidSwap 'pta'
  if ($r -eq 'SETTLED') { $settled = $true; break }
  Start-Sleep -Seconds 3
  if ((EvalStr 'pta' "String(window.__frames.some(f => f.msg && f.msg.type === 'settlement'))") -eq 'true') { $settled = $true; break }
}
if (-not $settled) { foreach ($i in 1..90) { Start-Sleep -Seconds 1; if ((EvalStr 'pta' "String(window.__frames.some(f => f.msg && f.msg.type === 'settlement'))") -eq 'true') { $settled = $true; break } } }
if (-not $settled) { Log 'FATAL: no forfeit settlement on A'; exit 1 }
$setl = EvalStr 'pta' 'window.__lastSettlement()'
Log "  A settlement: $setl (drop at $($bDropAt.ToString('HH:mm:ss')))"
Start-Sleep -Seconds 1
AB 'pta' @('screenshot', (Join-Path $dir 't8-7-forfeit-win-a.png')) | Out-Null

# ---- Stage 5: A rematch via settlement screen ----
Log 'stage5: A rematch'
ClickDesign 'pta' -170 -180
if (-not (WaitFor 'pta' "String(window.__outCount('queue_join') > 0)" 12 'pta auto queue_join')) { AB 'pta' @('screenshot', (Join-Path $dir 't8-8-rematch-fail.png')) | Out-Null; exit 1 }
Start-Sleep -Seconds 1
AB 'pta' @('screenshot', (Join-Path $dir 't8-8-rematch-queued-a.png')) | Out-Null

# ---- Stage 6: B give-up path -> exit to lobby ----
Log 'stage6: B give-up'
$waitSecs = 75 - [Math]::Round(((Get-Date) - $bDropAt).TotalSeconds)
if ($waitSecs -gt 0) { Log "  waiting $waitSecs s for B give-up timer"; Start-Sleep -Seconds $waitSecs }
AB 'ptb' @('screenshot', (Join-Path $dir 't8-9-b-giveup.png')) | Out-Null
ClickDesign 'ptb' 0 -440
Start-Sleep -Seconds 3
AB 'ptb' @('screenshot', (Join-Path $dir 't8-10-b-lobby.png')) | Out-Null

# ---- Stage 7: full match -> both settlements ----
Log 'stage7: pair again'
# A 仍在排队（再战入口）；B 从大厅开始匹配配对
ClickDesign 'ptb' 0 -100
if (-not (WaitFor 'ptb' "String(window.__frames.filter(f => f.msg && f.msg.type === 'match_started').length >= 2)" 20 'ptb new match_started')) { exit 1 }
$null = WaitFor 'pta' "String(window.__frames.filter(f => f.msg && f.msg.type === 'match_started').length >= 2)" 10 'pta new match_started'
Log "  match_started counts A=$(EvalStr 'pta' "String(window.__frames.filter(f => f.msg && f.msg.type === 'match_started').length)") B=$(EvalStr 'ptb' "String(window.__frames.filter(f => f.msg && f.msg.type === 'match_started').length)")"
foreach ($s in 'pta', 'ptb') {
  $cnt = EvalStr $s "String(window.__frames.filter(f => f.msg && f.msg.type === 'match_token').length)"
  $seat = EvalStr $s "String((window.__frames.filter(f => f.msg && f.msg.type === 'match_token')[$cnt - 1] || {msg:{payload:{seat:-1}}}).msg.payload.seat)"
  $null = EvalStr $s "window.__mySeat = $seat; window.__playedTurn = 0;"
  Log "  $s new seat=$seat (token count $cnt)"
}
Log 'stage7b: full match loop'
$done = $false
foreach ($i in 1..24) {
  $played = $false
  foreach ($s in 'ptb', 'pta') {
    $st = EvalStr $s "String(window.__frames.some(f => f.msg && f.msg.type === 'settlement'))"
    if ($st -eq 'true') { $done = $true; break }
    $r = PlayValidSwap $s
    if ($r -eq 'PLAYED') { $played = $true; break }
    if ($r -eq 'SETTLED') { $done = $true; break }
  }
  if ($done) { break }
  if (-not $played) { Log "  cycle $i nobody-played" }
}
foreach ($i in 1..100) {
  $a = EvalStr 'pta' "String(window.__frames.some(f => f.msg && f.msg.type === 'settlement'))"
  $b = EvalStr 'ptb' "String(window.__frames.some(f => f.msg && f.msg.type === 'settlement'))"
  if ($a -eq 'true' -and $b -eq 'true') { Log '  both settlements received'; break }
  Start-Sleep -Seconds 1
}
Start-Sleep -Seconds 1
$setlA = EvalStr 'pta' 'window.__lastSettlement()'
$setlB = EvalStr 'ptb' 'window.__lastSettlement()'
Log "  A settlement: $setlA"
Log "  B settlement: $setlB"
AB 'pta' @('screenshot', (Join-Path $dir 't8-11-final-a.png')) | Out-Null
AB 'ptb' @('screenshot', (Join-Path $dir 't8-11-final-b.png')) | Out-Null
# A 返回大厅；B 再战自动排队后取消
ClickDesign 'pta' 170 -180
Start-Sleep -Seconds 3
AB 'pta' @('screenshot', (Join-Path $dir 't8-12-a-lobby.png')) | Out-Null
ClickDesign 'ptb' -170 -180
$null = WaitFor 'ptb' "String(window.__outCount('queue_join') > 0)" 12 'ptb rematch queue_join'
Start-Sleep -Seconds 1
AB 'ptb' @('screenshot', (Join-Path $dir 't8-13-b-rematch.png')) | Out-Null
ClickDesign 'ptb' 0 -100
$null = WaitFor 'ptb' "String(window.__outCount('queue_cancel') > 0)" 12 'ptb queue_cancel'
AB 'ptb' @('screenshot', (Join-Path $dir 't8-14-b-cancelled.png')) | Out-Null
Log 'done'
