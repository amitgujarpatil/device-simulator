#!/bin/bash
# Device Simulator — Linux smart launcher
# Tries every available method in order until one works.

SCRIPT_DIR="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
BINARY="$SCRIPT_DIR/device-simulator-linux"
APPIMAGE="$SCRIPT_DIR/DeviceSimulator-linux-x86_64.AppImage"
INSTALLER="$SCRIPT_DIR/install-deps.sh"

log()  { echo -e "\033[32m[RUN]\033[0m $*"; }
warn() { echo -e "\033[33m[RUN]\033[0m $*" >&2; }
err()  { echo -e "\033[31m[RUN]\033[0m ERROR: $*" >&2; }

deps_ok() {
    [ -f "$BINARY" ] || return 1
    command -v ldd &>/dev/null || return 1
    ! ldd "$BINARY" 2>&1 | grep -q "not found"
}

# ── Method 1: binary (deps already present) ────────────────────────────────────
if deps_ok; then
    log "Launching binary directly…"
    chmod +x "$BINARY"
    exec "$BINARY" "$@"
fi

warn "Runtime libraries missing. Attempting auto-install…"

# ── Method 2: install deps, then run binary ────────────────────────────────────
if [ -f "$INSTALLER" ]; then
    chmod +x "$INSTALLER"
    if bash "$INSTALLER" "$BINARY"; then
        if deps_ok; then
            log "Launching binary after install…"
            chmod +x "$BINARY"
            exec "$BINARY" "$@"
        fi
    fi
    warn "Auto-install did not fully resolve dependencies."
fi

# ── Method 3: AppImage (FUSE mount) ────────────────────────────────────────────
if [ -f "$APPIMAGE" ]; then
    chmod +x "$APPIMAGE"

    # Test if FUSE is available
    if [ -e /dev/fuse ] || modprobe fuse 2>/dev/null; then
        log "Trying AppImage via FUSE…"
        if "$APPIMAGE" --appimage-version &>/dev/null 2>&1; then
            exec "$APPIMAGE" "$@"
        fi
    fi

    # ── Method 4: AppImage (extract-and-run, no FUSE needed) ──────────────────
    log "Trying AppImage in extract mode (no FUSE required)…"
    exec env APPIMAGE_EXTRACT_AND_RUN=1 "$APPIMAGE" "$@"
fi

# ── All methods failed ─────────────────────────────────────────────────────────
err "All launch methods failed. Install dependencies manually:"
err ""
err "  Ubuntu 20.04 / 22.04:"
err "    sudo apt-get install libwebkit2gtk-4.0-37 libgtk-3-0"
err ""
err "  Ubuntu 24.04:"
err "    sudo apt-get install libwebkit2gtk-4.1-0 libgtk-3-0"
err ""
err "  Fedora / RHEL:"
err "    sudo dnf install webkit2gtk4.0"
err ""
err "  Arch Linux:"
err "    sudo pacman -S webkit2gtk"
err ""
err "Then run:  $BINARY"
exit 1
