$ErrorActionPreference = 'Continue'
$dir = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify'
$log = Join-Path $dir 't8-probe.log'
function Log($m) { Add-Content -Path $log -Value ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss'), $m) }
Set-Content -Path $log -Value '=== probe ==='
function AB($s, $js) {
  $r = agent-browser --session $s eval $js 2>&1
  Log "$s <- $r"
  return $r
}
# 1) both windows alive + helpers?
foreach ($s in 'pta','ptb') {
  AB $s "JSON.stringify({url: location.pathname + location.search, bridge: typeof window.__poptrioNet, frames: (window.__frames||[]).length, helpers: typeof window.__poptrioHelpers})"
}
Log 'probe done'
Get-Content $log
