#!/bin/bash
# Builds a self-contained AppImage for Device Simulator (linux/amd64).
# Run inside: docker run --platform linux/amd64 golang:1.21-bookworm bash /workspace/build/build-appimage-v2.sh
set -euo pipefail

WORKSPACE=/workspace
OUTDIR=$WORKSPACE/build/bin
BINARY=$OUTDIR/device-simulator-linux
APPDIR=/tmp/DevSimAppDir

log()  { echo -e "\033[32m[APPIMAGE]\033[0m $*"; }
warn() { echo -e "\033[33m[APPIMAGE]\033[0m $*"; }

# ── 1. Deps ────────────────────────────────────────────────────────────────────
log "Installing build deps…"
apt-get update -q
apt-get install -y -q --no-install-recommends \
    libwebkit2gtk-4.0-dev libgtk-3-dev libglib2.0-dev \
    libatk1.0-dev libcairo2-dev libpango1.0-dev libgdk-pixbuf2.0-dev \
    libsoup2.4-dev libjavascriptcoregtk-4.0-dev \
    pkg-config build-essential wget squashfs-tools python3 patchelf file 2>&1 | tail -3

# ── 2. AppDir skeleton ─────────────────────────────────────────────────────────
log "Creating AppDir…"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" \
         "$APPDIR/usr/lib" \
         "$APPDIR/usr/share/icons/hicolor/256x256/apps" \
         "$APPDIR/usr/share/applications"

cp "$BINARY" "$APPDIR/usr/bin/device-simulator"
chmod +x "$APPDIR/usr/bin/device-simulator"
cp "$WORKSPACE/build/appicon.png" "$APPDIR/usr/share/icons/hicolor/256x256/apps/device-simulator.png"
cp "$WORKSPACE/build/appicon.png" "$APPDIR/device-simulator.png"

cat > "$APPDIR/device-simulator.desktop" << 'DESKTOP'
[Desktop Entry]
Name=Device Simulator
Exec=device-simulator
Icon=device-simulator
Type=Application
Categories=Utility;
DESKTOP
cp "$APPDIR/device-simulator.desktop" "$APPDIR/usr/share/applications/"

# ── 3. Bundle libraries (recursive ldd, skip base libc) ───────────────────────
log "Bundling shared libraries…"

declare -A SEEN
bundle() {
    local bin="$1"
    ldd "$bin" 2>/dev/null | awk '/=>/ && /\// { print $3 }' | while read -r lib; do
        lib=$(readlink -f "$lib" 2>/dev/null || echo "$lib")
        [ -f "$lib" ] || continue
        name=$(basename "$lib")
        case "$name" in
            libc.so*|libpthread.so*|libm.so*|libdl.so*|librt.so*|libgcc_s.so*|ld-linux*) continue ;;
        esac
        dest="$APPDIR/usr/lib/$name"
        if [ ! -f "$dest" ]; then
            cp -L "$lib" "$dest"
            bundle "$dest"
        fi
    done
}
bundle "$APPDIR/usr/bin/device-simulator"

# Also bundle any .so soname symlinks
for lib in "$APPDIR/usr/lib"/*.so.*; do
    [ -f "$lib" ] || continue
    bundle "$lib"
done

log "Libraries bundled: $(ls "$APPDIR/usr/lib" | wc -l)"

# ── 4. WebKit2GTK resources ───────────────────────────────────────────────────
log "Copying WebKit2GTK resources…"

# WebKit internal libs (injected bundle etc.)
for dir in $(find /usr/lib -maxdepth 3 -name "webkit2gtk-4.0" -type d 2>/dev/null); do
    mkdir -p "$APPDIR/usr/lib/webkit2gtk-4.0"
    cp -rL "$dir/." "$APPDIR/usr/lib/webkit2gtk-4.0/" 2>/dev/null || true
    # bundle deps of everything inside
    find "$APPDIR/usr/lib/webkit2gtk-4.0" -name "*.so*" | while read -r s; do bundle "$s"; done
    log "  webkit2gtk lib dir: $dir"
done

# WebKit subprocess helpers (WebKitWebProcess, WebKitNetworkProcess)
for dir in $(find /usr/libexec -maxdepth 3 -name "webkit2gtk*" -type d 2>/dev/null) \
           $(find /usr/lib/x86_64-linux-gnu -maxdepth 3 -name "webkit2gtk*" -type d 2>/dev/null); do
    mkdir -p "$APPDIR/usr/libexec/webkit2gtk-4.0"
    cp -rL "$dir/." "$APPDIR/usr/libexec/webkit2gtk-4.0/" 2>/dev/null || true
    find "$APPDIR/usr/libexec/webkit2gtk-4.0" -type f ! -name "*.so*" | while read -r f; do
        bundle "$f"
    done
    log "  webkit2gtk libexec: $dir"
done

# Also search usr/bin for webkit helpers
for helper in WebKitWebProcess WebKitNetworkProcess; do
    hpath=$(find /usr -name "$helper" -type f 2>/dev/null | head -1)
    if [ -n "$hpath" ]; then
        mkdir -p "$APPDIR/usr/libexec/webkit2gtk-4.0"
        cp "$hpath" "$APPDIR/usr/libexec/webkit2gtk-4.0/"
        bundle "$APPDIR/usr/libexec/webkit2gtk-4.0/$helper"
        log "  helper: $hpath"
    fi
done

# ── 5. GDK pixbuf loaders ─────────────────────────────────────────────────────
log "Copying GDK pixbuf loaders…"
for dir in $(find /usr/lib -maxdepth 4 -name "gdk-pixbuf-2.0" -type d 2>/dev/null); do
    mkdir -p "$APPDIR/usr/lib/gdk-pixbuf-2.0"
    cp -rL "$dir/." "$APPDIR/usr/lib/gdk-pixbuf-2.0/" 2>/dev/null || true
    log "  pixbuf: $dir"
done

# ── 6. GIO modules ────────────────────────────────────────────────────────────
log "Copying GIO modules…"
for dir in $(find /usr/lib -maxdepth 4 -name "gio" -type d 2>/dev/null); do
    mkdir -p "$APPDIR/usr/lib/gio"
    cp -rL "$dir/." "$APPDIR/usr/lib/gio/" 2>/dev/null || true
done

# ── 7. GTK immodules ──────────────────────────────────────────────────────────
for dir in $(find /usr/lib -maxdepth 4 -name "gtk-3.0" -type d 2>/dev/null); do
    mkdir -p "$APPDIR/usr/lib/gtk-3.0"
    cp -rL "$dir/." "$APPDIR/usr/lib/gtk-3.0/" 2>/dev/null || true
done

# ── 8. Fonts + theme hints (minimal) ─────────────────────────────────────────
mkdir -p "$APPDIR/usr/share/fonts"
cp -rL /usr/share/fonts/truetype 2>/dev/null "$APPDIR/usr/share/fonts/" || true

# ── 9. AppRun ─────────────────────────────────────────────────────────────────
log "Writing AppRun…"
cat > "$APPDIR/AppRun" << 'APPRUN'
#!/bin/sh
HERE="$(dirname "$(readlink -f "${0}")")"
export APPDIR="$HERE"

# Library path — bundled libs first
export LD_LIBRARY_PATH="$HERE/usr/lib:$HERE/usr/lib/webkit2gtk-4.0:${LD_LIBRARY_PATH:-}"

# XDG / icon / data paths
export XDG_DATA_DIRS="$HERE/usr/share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"

# GDK pixbuf
export GDK_PIXBUF_MODULE_FILE="$HERE/usr/lib/gdk-pixbuf-2.0/2.10.0/loaders.cache"
export GDK_PIXBUF_MODULEDIR="$HERE/usr/lib/gdk-pixbuf-2.0/2.10.0/loaders"

# GIO modules
export GIO_MODULE_DIR="$HERE/usr/lib/gio/modules"

# WebKit subprocess helpers
export WEBKIT_EXEC_PATH="$HERE/usr/libexec/webkit2gtk-4.0"
export WEBKIT_INJECTED_BUNDLE_PATH="$HERE/usr/lib/webkit2gtk-4.0"

# Force X11 backend (works on both X11 natively and Wayland via XWayland)
export GDK_BACKEND=x11
# Disable GPU compositing (avoids GPU driver issues in many environments)
export WEBKIT_DISABLE_COMPOSITING_MODE=1

exec "$HERE/usr/bin/device-simulator" "$@"
APPRUN
chmod +x "$APPDIR/AppRun"

# ── 10. Assemble AppImage ──────────────────────────────────────────────────────
log "Downloading AppImage runtime…"
wget -q -O /tmp/runtime \
    "https://github.com/AppImage/AppImageKit/releases/download/continuous/runtime-x86_64"

log "Building squashfs…"
mksquashfs "$APPDIR" /tmp/DevSim.squashfs \
    -root-owned -noappend -comp gzip 2>&1 | tail -3

log "Assembling AppImage…"
cat /tmp/runtime /tmp/DevSim.squashfs > "$OUTDIR/DeviceSimulator-linux-x86_64.AppImage"
chmod +x "$OUTDIR/DeviceSimulator-linux-x86_64.AppImage"

SIZE=$(du -sh "$OUTDIR/DeviceSimulator-linux-x86_64.AppImage" | cut -f1)
log "✓ AppImage ready: $OUTDIR/DeviceSimulator-linux-x86_64.AppImage ($SIZE)"

# ── 11. Copy helper scripts ────────────────────────────────────────────────────
cp "$WORKSPACE/build/linux/install-deps.sh" "$OUTDIR/"
cp "$WORKSPACE/build/linux/run.sh"          "$OUTDIR/"
chmod +x "$OUTDIR/install-deps.sh" "$OUTDIR/run.sh"

log "✓ All Linux deliverables in $OUTDIR:"
ls -lh "$OUTDIR/device-simulator-linux" \
        "$OUTDIR/DeviceSimulator-linux-x86_64.AppImage" \
        "$OUTDIR/install-deps.sh" \
        "$OUTDIR/run.sh"
