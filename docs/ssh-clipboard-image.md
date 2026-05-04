# SSH Clipboard Images for Codex

This repo now includes a small clipboard bridge that follows the same architecture as the March 27, 2026 Claude article, but without relying on Claude-specific slash commands.

## Binaries

- `ccimgd`: run this on your local machine. It reads a PNG from the local clipboard and serves it over TCP.
- `ccimg`: run this on the remote machine. It connects to the local daemon through an SSH reverse tunnel, saves the PNG, and prints the file path.

## Global install

The simplest global target on this machine is `~/.local/bin`, which is already in `PATH`.

From this repo:

```bash
./scripts/install-clipboard-bridge-global.sh
```

That installs:

- `~/.local/bin/ccimg`
- `~/.local/bin/ccimgd`

## Build

From this repo:

```bash
go build -o ./bin/ccimg ./cmd/ccimg
go build -o ./bin/ccimgd ./cmd/ccimgd
```

## Local machine setup

### macOS

Install `pngpaste`:

```bash
brew install pngpaste
```

Run the daemon:

```bash
./bin/ccimgd
```

### Linux

- Wayland: install `wl-paste`
- X11: install `xclip`

Run the same `./bin/ccimgd` command after the dependency is installed.

## SSH tunnel

Connect to the remote host with reverse forwarding:

```bash
ssh -R 9998:127.0.0.1:9998 your-server
```

## Remote usage

On the remote side:

```bash
ccimg
```

The command prints a PNG path such as `/tmp/ccimg-123456.png`.

## Using it with Codex

Once `ccimg` prints the saved file path, use that image file in the session the same way you would use any other local image file available to the agent.

This is the important difference from the original Claude workflow:

- no `~/.claude/commands/paste-image.md`
- no Claude `Read` tool
- the bridge only solves clipboard-over-SSH transport
