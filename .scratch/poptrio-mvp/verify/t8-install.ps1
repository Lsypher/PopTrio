$ErrorActionPreference = 'Continue'
$dir = 'c:\Users\nxm\Desktop\PopTrio\.scratch\poptrio-mvp\verify'
$log = Join-Path $dir 't8-probe.log'
function Log($m) { Add-Content -Path $log -Value ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss'), $m) }
function AB($s, $js) {
  $r = agent-browser --session $s eval $js 2>&1
  Log "$s <- $r"
  return $r
}
$i7 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content (Join-Path $dir 't7-helpers.js') -Raw)))
$i8 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content (Join-Path $dir 't8-helpers.js') -Raw)))
foreach ($s in 'pta','ptb') {
  Log "installing on $s"
  AB $s "eval(atob('$i7')); eval(atob('$i8')); 'installed'"
}
foreach ($s in 'pta','ptb') {
  AB $s "JSON.stringify({helpers: typeof window.__poptrioHelpers, waitTurn8: typeof window.__waitTurn8, lastSettle: typeof window.__lastSettlement})"
}
