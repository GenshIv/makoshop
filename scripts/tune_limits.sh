#!/usr/bin/env bash
# tune_limits.sh — показать/применить/восстановить сетевые и системные лимиты
# для высокой конкурентности Go-сервера MakoShop.
#
# Использование:
#   sudo ./tune_limits.sh show
#   sudo ./tune_limits.sh apply
#   sudo ./tune_limits.sh restore
#
# Внимание:
# - apply перезаписывает часть параметров ядра и создаёт бэкап.
# - restore возвращает оригинальные значения из бэкапа.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKUP_FILE="/etc/sysctl.d/99-makoshop-backup.conf"
APPLIED_FILE="/etc/sysctl.d/99-makoshop.conf"

show_value() {
    local key="$1"
    local val
    val=$(sysctl -n "$key" 2>/dev/null || echo "N/A")
    printf "%-45s = %s\n" "$key" "$val"
}

show() {
    echo "=== Current Limits ==="
    echo ""
    echo "--- File descriptors ---"
    echo "ulimit -n (current shell):"
    ulimit -n
    echo ""
    show_value "fs.file-max"
    echo ""
    echo "--- TCP buffers ---"
    show_value "net.ipv4.tcp_rmem"
    show_value "net.ipv4.tcp_wmem"
    show_value "net.ipv4.tcp_mem"
    echo ""
    echo "--- Connection backlog ---"
    show_value "net.core.somaxconn"
    echo ""
    echo "--- Port range ---"
    show_value "net.ipv4.ip_local_port_range"
    echo ""
    echo "--- TIME_WAIT reuse ---"
    show_value "net.ipv4.tcp_tw_reuse"
    echo ""
    echo "--- Recommended values (for high concurrency) ---"
    echo ""
    echo "fs.file-max                    = 2097152"
    echo "net.ipv4.tcp_rmem              = 4096 87380 6291456"
    echo "net.ipv4.tcp_wmem              = 4096 65536 6291456"
    echo "net.ipv4.tcp_mem               = 196608 262144 393216"
    echo "net.core.somaxconn             = 8192"
    echo "net.ipv4.ip_local_port_range   = 1024 65535"
    echo "net.ipv4.tcp_tw_reuse          = 1"
    echo ""
    echo "ulimit -n (per-process):       131072 или выше"
    echo ""
}

backup() {
    if [[ -f "$APPLIED_FILE" ]]; then
        cp "$APPLIED_FILE" "$BACKUP_FILE"
        echo "Backed up current config to $BACKUP_FILE"
    else
        # Save current runtime values
        echo "# Backup created by tune_limits.sh on $(date -Iseconds)" > "$BACKUP_FILE"
        for key in fs.file-max net.ipv4.tcp_rmem net.ipv4.tcp_wmem net.ipv4.tcp_mem net.core.somaxconn net.ipv4.ip_local_port_range net.ipv4.tcp_tw_reuse; do
            val=$(sysctl -n "$key" 2>/dev/null || echo "")
            if [[ -n "$val" ]]; then
                echo "$key = $val" >> "$BACKUP_FILE"
            fi
        done
        echo "Created backup of current sysctl values to $BACKUP_FILE"
    fi
}

apply() {
    echo "=== Applying recommended limits ==="

    if [[ $EUID -ne 0 ]]; then
        echo "ERROR: This script must be run as root (use sudo)."
        exit 1
    fi

    backup

    cat > "$APPLIED_FILE" <<EOF
# MakoShop tuning — applied by tune_limits.sh on $(date -Iseconds)
fs.file-max = 2097152
net.ipv4.tcp_rmem = 4096 87380 6291456
net.ipv4.tcp_wmem = 4096 65536 6291456
net.ipv4.tcp_mem = 196608 262144 393216
net.core.somaxconn = 8192
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
EOF

    sysctl -p "$APPLIED_FILE"

    echo ""
    echo "Applied sysctl settings from $APPLIED_FILE"
    echo ""
    echo "IMPORTANT: ulimit -n is per-shell/per-process."
    echo "For your current shell:"
    echo "  ulimit -n 131072"
    echo ""
    echo "For systemd services (e.g. makoshop.service):"
    echo "  LimitNOFILE=131072"
    echo ""
    echo "Run 'sudo ./tune_limits.sh show' to verify."
}

restore() {
    echo "=== Restoring previous limits ==="

    if [[ $EUID -ne 0 ]]; then
        echo "ERROR: This script must be run as root (use sudo)."
        exit 1
    fi

    if [[ ! -f "$BACKUP_FILE" ]]; then
        echo "ERROR: No backup file found at $BACKUP_FILE"
        exit 1
    fi

    sysctl -p "$BACKUP_FILE"
    rm -f "$APPLIED_FILE"

    echo ""
    echo "Restored sysctl settings from $BACKUP_FILE"
    echo "Removed $APPLIED_FILE"
    echo ""
    echo "Run 'sudo ./tune_limits.sh show' to verify."
}

case "${1:-show}" in
    show)
        show
        ;;
    apply)
        apply
        ;;
    restore)
        restore
        ;;
    *)
        echo "Usage: $0 {show|apply|restore}"
        exit 1
        ;;
esac
