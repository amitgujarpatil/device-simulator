# Device Simulator — Linux Build & Install Guide

A complete reference for building the app from source and installing it as a
native desktop application on Linux.

---

## Prerequisites

| Tool | Minimum version | Install |
|------|----------------|---------|
| Go | 1.21 | [go.dev/dl](https://go.dev/dl) |
| Wails CLI | v2.9.1 | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| WebKit2GTK (dev) | 4.0 **or** 4.1 | see below |
| GTK3 (dev) | 3.x | see below |

### Install system libraries

**Ubuntu 20.04 / 22.04**
```bash
sudo apt-get install -y libwebkit2gtk-4.0-dev libgtk-3-dev \
  libglib2.0-dev pkg-config build-essential
```

**Ubuntu 24.04 / 25.x**
```bash
sudo apt-get install -y libwebkit2gtk-4.1-dev libgtk-3-dev \
  libglib2.0-dev pkg-config build-essential
```

**Fedora / RHEL / CentOS**
```bash
sudo dnf install -y webkit2gtk4.0-devel gtk3-devel pkg-config gcc
```

**Arch Linux**
```bash
sudo pacman -S webkit2gtk gtk3 pkg-config base-devel
```

**openSUSE**
```bash
sudo zypper install -y libwebkit2gtk-4_0-devel gtk3-devel pkg-config
```

> **Verify** pkg-config can see the libraries before building:
> ```bash
> pkg-config --libs webkit2gtk-4.1 2>/dev/null || pkg-config --libs webkit2gtk-4.0
> ```

---

## Build from source

```bash
# Clone the repo and switch to the correct branch
git clone https://github.com/amitgujarpatil/device-simulator.git
cd device-simulator
git checkout web-server

# Build the Linux binary (from project root)
~/go/bin/wails build -platform linux/amd64
```

Output lands at:
```
build/bin/device-simulator          ← the compiled binary
```

Build time is ~5 seconds on a modern machine. No internet access needed after
`go mod download` (all deps are cached in the Go module cache).

---

## Copy binary to the linux/ folder

The `build/linux/` folder contains the launcher and dependency installer scripts.
Place the compiled binary there so the scripts can find it:

```bash
cp build/bin/device-simulator build/linux/device-simulator-linux
chmod +x build/linux/device-simulator-linux
```

---

## Install as a desktop application

Running the steps below registers the app in your system's application launcher
(GNOME Activities, KDE app menu, etc.) so you can open it by clicking its icon
just like any other installed app.

### Step 1 — Install the binary

```bash
mkdir -p ~/.local/bin
cp build/linux/device-simulator-linux ~/.local/bin/device-simulator
chmod +x ~/.local/bin/device-simulator
```

### Step 2 — Install the icon (all standard sizes)

The source icon is `build/appicon.png` (1024×1024 PNG). Resize and install it
at every size the hicolor theme expects (requires Pillow: `pip install Pillow`):

```bash
ICON_SRC="build/appicon.png"

for size in 16 32 48 64 128 256 512 1024; do
    dir="$HOME/.local/share/icons/hicolor/${size}x${size}/apps"
    mkdir -p "$dir"
    python3 -c "
from PIL import Image
img = Image.open('$ICON_SRC').resize(($size, $size), Image.LANCZOS)
img.save('$dir/device-simulator.png')
"
done
```

### Step 3 — Create the .desktop entry

```bash
mkdir -p ~/.local/share/applications

cat > ~/.local/share/applications/device-simulator.desktop << 'EOF'
[Desktop Entry]
Version=1.0
Type=Application
Name=Device Simulator
GenericName=IoT Device Simulator
Comment=Simulate IoT device telemetry — GPS, OBD, MQTT
Exec=/home/YOUR_USERNAME/.local/bin/device-simulator
Icon=device-simulator
Terminal=false
Categories=Development;Utility;
Keywords=simulator;iot;mqtt;gps;obd;telemetry;
StartupNotify=true
StartupWMClass=device-simulator
EOF
```

> Replace `YOUR_USERNAME` with your actual Linux username, or use `$HOME` in
> the path.

### Step 4 — Refresh icon and desktop caches

```bash
gtk-update-icon-cache -f -t ~/.local/share/icons/hicolor
update-desktop-database ~/.local/share/applications
```

If the app doesn't appear in the launcher immediately, **log out and log back
in** once.

---

## Verify runtime dependencies

Run the bundled checker any time to confirm the binary can find all its shared
libraries:

```bash
bash build/linux/install-deps.sh ~/.local/bin/device-simulator
```

If libraries are missing, the script will detect your distro and install them
automatically.

---

## Quick launch (without desktop install)

```bash
# Smart launcher — tries binary, auto-installs deps if needed, falls back to AppImage
bash build/linux/run.sh

# Or run the binary directly
build/linux/device-simulator-linux
```

---

## File map

```
build/
├── appicon.png                  ← 1024×1024 source icon (used for icon install)
├── bin/
│   └── device-simulator         ← compiled binary (output of wails build)
└── linux/
    ├── device-simulator-linux   ← binary copy for distribution
    ├── install-deps.sh          ← auto-installs WebKit/GTK on any supported distro
    ├── run.sh                   ← smart launcher (binary → deps install → AppImage)
    └── BUILD-AND-INSTALL.md     ← this file
```

---

## Installed file locations (after desktop install)

| What | Path |
|------|------|
| Binary | `~/.local/bin/device-simulator` |
| Desktop entry | `~/.local/share/applications/device-simulator.desktop` |
| Icons | `~/.local/share/icons/hicolor/<size>x<size>/apps/device-simulator.png` |

To **uninstall**, remove those three locations:

```bash
rm ~/.local/bin/device-simulator
rm ~/.local/share/applications/device-simulator.desktop
rm ~/.local/share/icons/hicolor/*/apps/device-simulator.png
gtk-update-icon-cache -f -t ~/.local/share/icons/hicolor
update-desktop-database ~/.local/share/applications
```

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| App not in launcher after install | Log out and back in, or run `update-desktop-database ~/.local/share/applications` |
| `ldd: not found` errors on launch | Run `install-deps.sh` (auto-detects distro) |
| Wrong webkit version | Run `pkg-config --modversion webkit2gtk-4.1` — Wails 2.9.x supports both 4.0 and 4.1 |
| Icon not showing | Run `gtk-update-icon-cache -f -t ~/.local/share/icons/hicolor` |
| Binary crashes on Wayland | Try `GDK_BACKEND=x11 device-simulator` or ensure `XDG_SESSION_TYPE=x11` |
