FROM golang:1.25-bookworm

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    g++-mingw-w64-x86-64 \
    gcc-mingw-w64-x86-64 \
    mingw-w64 \
    nodejs \
    npm \
    nsis \
    pkg-config \
    zip \
    && rm -rf /var/lib/apt/lists/*

RUN go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0

ENV PATH="/go/bin:${PATH}" \
    GOOS=windows \
    GOARCH=amd64 \
    CGO_ENABLED=1 \
    CC=x86_64-w64-mingw32-gcc \
    CXX=x86_64-w64-mingw32-g++

WORKDIR /workspace

CMD ["bash"]
