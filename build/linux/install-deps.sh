#!/bin/bash
# Device Simulator — Linux dependency installer
# Supports: Ubuntu/Debian · Fedora/RHEL/CentOS · Arch Linux · openSUSE
# Retries every network/package operation up to 3 times with backoff.

set -euo pipefail

BINARY="${1:-$(dirname "$(readlink -f "$0")")/device-simulator-linux}"
MAX_RETRIES=3

# ── Helpers ────────────────────────────────────────────────────────────────────
log()  { echo -e "\033[32m[DEPS]\033[0m $*"; }
warn() { echo -e "\033[33m[DEPS]\033[0m WARNING: $*" >&2; }
err()  { echo -e "\033[31m[DEPS]\033[0m ERROR: $*" >&2; }

retry() {
    local delay=5 attempt=0
    while [ $attempt -lt $MAX_RETRIES ]; do
        if "$@"; then return 0; fi
        attempt=$((attempt + 1))
        [ $attempt -lt $MAX_RETRIES ] && { warn "Attempt $attempt failed — retrying in ${delay}s…"; sleep $delay; delay=$((delay * 2)); }
    done
    err "Command failed after $MAX_RETRIES attempts: $*"; return 1
}

# Run with sudo only if we are not already root
SUDO=""
[ "$(id -u)" != "0" ] && SUDO="sudo"

# ── Dep check ─────────────────────────────────────────────────────────────────
deps_ok() {
    [ -f "$BINARY" ] || return 1
    command -v ldd &>/dev/null || return 1
    ! ldd "$BINARY" 2>&1 | grep -q "not found"
}

# ── Distro installers ──────────────────────────────────────────────────────────
install_apt() {
    log "Detected Debian/Ubuntu — using apt-get"

    # Pick the right webkit2gtk package for this Ubuntu/Debian release
    WEBKIT_PKG=""
    retry $SUDO apt-get update -qq
    for pkg in libwebkit2gtk-4.1-0 libwebkitgtk-6.0-4 libwebkit2gtk-4.0-37; do
        if apt-cache show "$pkg" &>/dev/null 2>&1; then
            WEBKIT_PKG="$pkg"; break
        fi
    done
    [ -z "$WEBKIT_PKG" ] && { err "Could not find webkit2gtk package for this distro"; return 1; }
    log "WebKit package: $WEBKIT_PKG"

    retry $SUDO apt-get install -y --no-install-recommends \
        "$WEBKIT_PKG" \
        libgtk-3-0 \
        libglib2.0-0 \
        libatk1.0-0 \
        libcairo2 \
        libpango-1.0-0 \
        libgdk-pixbuf2.0-0 \
        libsoup2.4-1 \
        ca-certificates \
        xdg-utils
}

install_dnf() {
    log "Detected Fedora/RHEL — using dnf"
    WEBKIT_PKG="webkit2gtk4.0"
    dnf list available webkit2gtk4.0 &>/dev/null 2>&1 || WEBKIT_PKG="webkit2gtk3"
    retry $SUDO dnf install -y "$WEBKIT_PKG" gtk3 glib2 atk cairo pango gdk-pixbuf2 libsoup xdg-utils ca-certificates
}

install_yum() {
    log "Detected CentOS/RHEL (yum)"
    retry $SUDO yum install -y webkitgtk4 gtk3 glib2 atk cairo pango gdk-pixbuf2 libsoup xdg-utils ca-certificates
}

install_pacman() {
    log "Detected Arch Linux — using pacman"
    retry $SUDO pacman -Sy --noconfirm webkit2gtk gtk3 glib2 atk cairo pango gdk-pixbuf2 libsoup xdg-utils ca-certificates
}

install_zypper() {
    log "Detected openSUSE — using zypper"
    retry $SUDO zypper install -y \
        libwebkit2gtk-4_0-37 libgtk-3-0 libglib-2_0-0 libatk-1_0-0 \
        libcairo2 libpango-1_0-0 libgdk_pixbuf-2_0-0 libsoup-2_4-1 \
        xdg-utils ca-certificates
}

# ── Main ───────────────────────────────────────────────────────────────────────
main() {
    log "Checking dependencies for: $BINARY"

    if deps_ok; then
        log "✓ All dependencies already satisfied — no install needed."
        exit 0
    fi

    log "Missing libraries detected:"
    ldd "$BINARY" 2>&1 | grep "not found" | sed 's/^/  /' || true

    if   command -v apt-get &>/dev/null; then install_apt
    elif command -v dnf     &>/dev/null; then install_dnf
    elif command -v yum     &>/dev/null; then install_yum
    elif command -v pacman  &>/dev/null; then install_pacman
    elif command -v zypper  &>/dev/null; then install_zypper
    else
        err "No supported package manager found."
        err "Please install WebKit2GTK + GTK3 manually, then re-run:"
        err "  Ubuntu/Debian : sudo apt-get install libwebkit2gtk-4.0-37 libgtk-3-0"
        err "  Fedora        : sudo dnf install webkit2gtk4.0"
        err "  Arch          : sudo pacman -S webkit2gtk"
        err "  openSUSE      : sudo zypper install libwebkit2gtk-4_0-37"
        exit 1
    fi

    log "Verifying…"
    if deps_ok; then
        log "✓ All dependencies installed successfully!"
    else
        warn "Some libraries may still be missing:"
        ldd "$BINARY" 2>&1 | grep "not found" | sed 's/^/  /' || true
        warn "Try rebooting or running: sudo ldconfig"
    fi
}

main
