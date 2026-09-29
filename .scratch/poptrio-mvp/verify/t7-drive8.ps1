# PopTrio issue 07 driver v8: pre-installed helpers + fresh windows
$ErrorActionPreference = 'Continue'
$dir = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify'
$log = Join-Path $dir 't7-run8.log'
function Log($m) { Add-Content -Path $log -Value ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss.fff'), $m) }
Set-Content -Path $log -Value '=== t7 run8 ==='

function AB($session, $cmd) { agent-browser --session $session @cmd }
function EvalStr($session, $js) {
  $r = (agent-browser --session $session eval $js)
  if ($null -eq $r) { return '' }
  return $r.Replace('\"', '"').Trim('"')
}

$install = Get-Content (Join-Path $dir 't7-helpers.js') -Raw

Log 'fresh windows'
agent-browser close --all 2>&1 | Out-Null
Start-Sleep -Seconds 4
$init = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify\ws-log-init.js'
AB 'pta' @('open', '--init-script', $init, 'http://localhost:7456/') | Out-Null
Start-Sleep -Seconds 6
AB 'ptb' @('open', '--init-script', $init, 'http://localhost:7456/') | Out-Null
Start-Sleep -Seconds 3

foreach ($s in 'pta', 'ptb') {
  $null = EvalStr $s $install
  Log "pre-installed helpers on $s"
}

Log 'queue both'
foreach ($s in 'pta', 'ptb') {
  AB $s @('mouse', 'move', '632', '345') | Out-Null
  AB $s @('mouse', 'down', 'left') | Out-Null
  AB $s @('mouse', 'up', 'left') | Out-Null
}

$ok = $false
foreach ($i in 1..20) {
  Start-Sleep -Milliseconds 700
  $a = EvalStr 'pta' "String(window.__frames.some(f => f.msg && f.msg.type === 'match_started'))"
  $b = EvalStr 'ptb' "String(window.__frames.some(f => f.msg && f.msg.type === 'match_started'))"
  if ($a -eq 'true' -and $b -eq 'true') { $ok = $true; break }
}
if (-not $ok) { Log 'FATAL: no match_started'; exit 1 }
Log 'match started'

foreach ($s in 'pta', 'ptb') {
  $seat = EvalStr $s "String((window.__frames.find(f => f.msg && f.msg.type === 'match_token') || {msg:{payload:{seat:-1}}}).msg.payload.seat)"
  $null = EvalStr $s "window.__mySeat = $seat; window.__playedTurn = 0;"
  Log "seat=$seat on $s"
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
    AB $s @('wait', '500') | Out-Null
    AB $s @('mouse', 'move', "$($v[5])", "$($v[6])") | Out-Null
    AB $s @('mouse', 'down', 'left') | Out-Null
    AB $s @('mouse', 'up', 'left') | Out-Null
    AB $s @('wait', '120') | Out-Null
    AB $s @('mouse', 'move', "$($v[7])", "$($v[8])") | Out-Null
    AB $s @('mouse', 'down', 'left') | Out-Null
    AB $s @('mouse', 'up', 'left') | Out-Null
    AB $s @('wait', '250') | Out-Null
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
