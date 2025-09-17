# Common helpers inlined before the role body.

timestamp() { date '+%Y-%m-%d %H:%M:%S'; }
log() { echo "$(timestamp) | $*"; }

schedule_reboot() {
  local secs="${1:-5}"
  log "Scheduling reboot in ${secs}s (systemd-run)…"
  systemd-run --on-active="${secs}s" /sbin/reboot || {
    log "systemd-run unavailable, falling back to 'shutdown -r'…"
    shutdown -r +"$(( (secs+59)/60 ))" || true
  }
}

# Make a robust, idempotent NM static config for eth0.
configure_static_ip() {
  local cidr="$1" gw="$2" dns="$3"

  log "Configuring static IP (eth0) -> ${cidr} gw=${gw} dns=${dns}"

  # If dhcpcd exists, ensure it won't fight NetworkManager
  if systemctl list-unit-files 2>/dev/null | grep -q '^dhcpcd\.service'; then
    systemctl disable --now dhcpcd.service 2>/dev/null || true
  fi

  if ! command -v nmcli >/dev/null 2>&1; then
    log "NetworkManager/nmcli not found; cannot set static IP this way."
    return 0
  fi

  export NM_CLI_NO_COLOR=1

  # Turn off all other connections bound to eth0 (e.g. 'Wired connection 1')
  # and make sure they don't autoconnect.
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    if [ "$name" != "eth0-static" ]; then
      nmcli con modify "$name" connection.autoconnect no 2>/dev/null || true
      nmcli con down "$name" 2>/dev/null || true
    fi
  done < <(nmcli -t -f NAME,DEVICE con show 2>/dev/null | awk -F: '$2=="eth0"{print $1}')

  # Create or update our static profile
  if ! nmcli -t -f NAME con show 2>/dev/null | grep -Fxq "eth0-static"; then
    nmcli con add type ethernet ifname eth0 con-name "eth0-static" \
      ipv4.method manual ipv4.addresses "$cidr" ipv4.gateway "$gw" \
      ipv4.dns "$dns" ipv6.method ignore autoconnect yes || true
  fi

  # Ensure correct settings and make it highest priority
  nmcli con modify "eth0-static" \
    connection.autoconnect yes \
    connection.autoconnect-priority 100 \
    ipv4.method manual \
    ipv4.addresses "$cidr" \
    ipv4.gateway "$gw" \
    ipv4.dns "$dns" \
    ipv6.method ignore 2>/dev/null || true

  # Bring the static profile up now; if device is busy, force reapply
  if ! nmcli con up "eth0-static" >/dev/null 2>&1; then
    nmcli dev disconnect eth0 2>/dev/null || true
    nmcli dev connect eth0 2>/dev/null || true
    nmcli con up "eth0-static" 2>/dev/null || true
  fi

  # Final status for the log
  nmcli d show eth0 2>/dev/null | sed 's/^/  /' || true
  ip -brief addr | sed 's/^/  /' || true
}
