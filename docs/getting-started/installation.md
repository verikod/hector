# Installation

Hector is distributed as a single binary with no runtime dependencies. Choose your preferred installation method below.

## Requirements

- Go 1.24+ (for `go install` method only)
- Linux, macOS, or Windows (amd64 or arm64)

## Go Install (Recommended)

Install the latest version directly from source:

```bash
go install github.com/verikod/hector/cmd/hector@latest
```

Verify installation:

```bash
hector version
```

## Binary Download

Download pre-built binaries from [GitHub Releases](https://github.com/verikod/hector/releases):

### Linux (amd64)

```bash
curl -LO https://github.com/verikod/hector/releases/latest/download/hector_linux_amd64.tar.gz
tar -xzf hector_linux_amd64.tar.gz
sudo mv hector /usr/local/bin/
```

### Linux (arm64)

```bash
curl -LO https://github.com/verikod/hector/releases/latest/download/hector_linux_arm64.tar.gz
tar -xzf hector_linux_arm64.tar.gz
sudo mv hector /usr/local/bin/
```

### macOS (Intel)

```bash
curl -LO https://github.com/verikod/hector/releases/latest/download/hector_darwin_amd64.tar.gz
tar -xzf hector_darwin_amd64.tar.gz
sudo mv hector /usr/local/bin/
```

### macOS (Apple Silicon)

```bash
curl -LO https://github.com/verikod/hector/releases/latest/download/hector_darwin_arm64.tar.gz
tar -xzf hector_darwin_arm64.tar.gz
sudo mv hector /usr/local/bin/
```

### Windows

Download `hector_windows_amd64.zip` from the [releases page](https://github.com/verikod/hector/releases), extract it, and add the directory to your PATH.

## Docker

Run Hector in a container:

```bash
docker pull ghcr.io/verikod/hector:latest
docker run -p 8080:8080 -e OPENAI_API_KEY=your-key ghcr.io/verikod/hector:latest
```

Or build locally:

```bash
git clone https://github.com/verikod/hector.git
cd hector
docker build -t hector .
docker run -p 8080:8080 -e OPENAI_API_KEY=your-key hector
```

## Build from Source

Clone and build:

```bash
git clone https://github.com/verikod/hector.git
cd hector
make build
./bin/hector version
```

Install to system:

```bash
make install
```

## Verify Installation

Check that Hector is correctly installed:

```bash
$ hector version
Hector version dev
```

View available commands:

```bash
$ hector --help
```

