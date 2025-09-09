# Save as show-ip.ps1  (plain UTF-8)
param(
  [string]$InterfaceName = "Wi-Fi",
  [int]$IntervalSeconds = 2
)

while ($true) {
  Clear-Host
  $a = Get-NetAdapter -InterfaceAlias $InterfaceName -ErrorAction SilentlyContinue
  if (-not $a) {
    Write-Host ("Interface '{0}' not found" -f $InterfaceName)
  }
  elseif ($a.Status -ne 'Up') {
    Write-Host ("Interface: {0} | Status: {1}" -f $InterfaceName, $a.Status)
    Write-Host "IPv4: (disconnected)"
  }
  else {
    $ip = (Get-NetIPConfiguration -InterfaceAlias $InterfaceName -ErrorAction SilentlyContinue).IPv4Address.IPAddress |
          Where-Object { $_ -notmatch '^169\.254\.' }
    Write-Host ("Interface: {0} | Status: Up" -f $InterfaceName)
    if ($ip) {
      Write-Host ("IPv4: {0}" -f ($ip -join ', '))
    } else {
      Write-Host "IPv4: (no address assigned)"
    }
  }
  Start-Sleep -Seconds $IntervalSeconds
}
