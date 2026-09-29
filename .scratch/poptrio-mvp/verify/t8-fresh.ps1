$ErrorActionPreference = 'Continue'
$dir = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify'
$log = Join-Path $dir 't8-fresh.log'
function Log($m) { Add-Content -Path $log -Value ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss.fff'), $m) }
Set-Content -Path $log -Value '=== t8 fresh start ==='

# 1) close everything, kill automation chrome
agent-browser close --all 2>&1 | Out-Null
Start-Sleep -Seconds 3
$killed = Get-WmiObject Win32_Process -Filter "Name='chrome.exe'" | Where-Object { $_.CommandLine -match 'agent-browser' }
$killed | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
Log ("killed automation chrome: " + @($killed).Count)
Start-Sleep -Seconds 3

# 2) fresh windows
agent-browser --session pta open --init-script (Join-Path $dir 'ws-log-init.js') 'http://localhost:7456/?debug=1' 2>&1 | Out-Null
Start-Sleep -Seconds 7
agent-browser --session ptb open --init-script (Join-Path $dir 'ws-log-init.js') 'http://localhost:7456/?debug=1' 2>&1 | Out-Null
Start-Sleep -Seconds 6

# 3) install helpers via base64 (single eval, proven pattern)
$i7 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content (Join-Path $dir 't7-helpers.js') -Raw)))
$i8 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content (Join-Path $dir 't8-helpers.js') -Raw)))
foreach ($s in 'pta','ptb') {
  $r = agent-browser --session $s eval "eval(atob('$i7')); eval(atob('$i8')); 'ok|' + window.__frames.length + '|' + typeof window.__poptrioNet" 2>&1
  Log "$s install: $r"
}

# 4) state check: frames must be 0 before any click
foreach ($s in 'pta','ptb') {
  $r = agent-browser --session $s eval "JSON.stringify({frames: window.__frames.length, url: location.pathname + location.search})" 2>&1
  Log "$s state: $r"
}
Log 'fresh done'
Get-Content $log
