# ────────────────────────────────────────────────────────────────────────────
# Device Simulator — Linux builder image
#
# Build the image:
#   docker build -t device-sim-builder .
#
# Produce a Linux binary (outputs to ./build/bin/):
#   docker run --rm -v "$(pwd)/build/bin":/out device-sim-builder
#
# Or build + copy in one shot (from project root):
#   docker build -t device-sim-builder . && \
#   docker run --rm -v "$(pwd)/build/bin":/out device-sim-builder
# ────────────────────────────────────────────────────────────────────────────

FROM ubuntu:22.04

# Non-interactive apt
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC

# ── System build deps ────────────────────────────────────────────────────────
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    git \
    pkg-config \
    libgtk-3-dev \
    libwebkit2gtk-4.0-dev \
    libayatana-appindicator3-dev \
    upx-ucl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# ── Go 1.21 ──────────────────────────────────────────────────────────────────
ARG GO_VERSION=1.21.13
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
    | tar -xz -C /usr/local
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH=/root/go
ENV PATH="${GOPATH}/bin:${PATH}"

# ── Node.js 20 (needed by Wails for frontend build) ──────────────────────────
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# ── Wails v2.9.1 ─────────────────────────────────────────────────────────────
RUN go install github.com/wailsapp/wails/v2/cmd/wails@v2.9.1

# ── Copy project source ───────────────────────────────────────────────────────
WORKDIR /build
COPY . .

# Pre-download Go modules so the build is fully offline after image creation
RUN go mod download

# ── Build ─────────────────────────────────────────────────────────────────────
# Produces /build/build/bin/device-simulator-linux
RUN wails build -platform linux/amd64 -o device-simulator-linux

# ── Copy output to mounted /out ───────────────────────────────────────────────
# When run with -v "$(pwd)/build/bin":/out the binary lands in the host's
# build/bin directory alongside the macOS and Windows artifacts.
CMD cp -v /build/build/bin/device-simulator-linux /out/device-simulator-linux \
    && echo "✓ Linux binary written to build/bin/device-simulator-linux"
