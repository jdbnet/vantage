# Vantage

Vantage is a local-first SSH, VNC, and RDP client. Use the desktop app on your computer, and optionally run a small server (vantaged) if you want the same inventory on more than one machine.

VNC and RDP go through [Apache Guacamole's guacd](https://guacamole.apache.org/doc/gug/guacamole-docker.html). Install guacd yourself and point Vantage at it in Settings (`host:4822`). Vantage does not ship guacd.

Downloads: [GitHub Releases](https://github.com/jdbnet/vantage/releases).

## Install the desktop app

### Windows

Download the installer (`vantage_*_windows_amd64-setup.exe`) from the latest release and run it.

### macOS

Download the `.zip`, open it, and drag **Vantage** to Applications. The app is unsigned, so you may need to right-click and choose Open the first time.

### Linux (.deb)

Install the Debian package from the release, or from our APT repository:

```bash
sudo apt install ./vantage_*_amd64.deb
```

```bash
curl -fsSL https://apt.jdbnet.co.uk/install/stable.sh | sudo bash
sudo apt update
sudo apt install vantage
```

The package depends on GTK 3 and WebKitGTK.

### Linux (download and run)

Download `vantage_*_linux_amd64`, mark it executable, and run it:

```bash
chmod +x vantage_*_linux_amd64
./vantage_*_linux_amd64
```

On first launch it copies itself to `~/.local/share/vantage/`, adds a **Vantage** entry to your application menu, and starts from there. No root required. Needs GTK 3 and WebKitGTK 4.1.

## First launch

1. Open Vantage and create an operator account.
2. Add identities (passwords or SSH keys) and hosts.
3. For VNC or RDP, install guacd and set **guacd address** in Settings, usually `127.0.0.1:4822`.

A typical guacd container on the same machine as the desktop app:

```bash
docker run -d --name guacd -p 4822:4822 guacamole/guacd:1.5.5
```

## Optional: vantaged server

vantaged is the same app as a headless server with a browser UI. Use it when you want a shared inventory, or to open sessions from a browser. It is published as a Docker image, not a standalone binary.

```bash
docker run -d --name vantaged \
  -p 7687:7687 \
  -v vantaged-data:/data \
  ghcr.io/jdbnet/vantaged:latest
```

Then open `http://127.0.0.1:7687` and create the operator account.

Example Compose file with guacd on the same network (set Settings **guacd address** to `guacd:4822` inside this stack):

```yaml
services:
  vantaged:
    image: ghcr.io/jdbnet/vantaged:latest
    ports:
      - "7687:7687"
    volumes:
      - vantaged-data:/data
  guacd:
    image: guacamole/guacd:1.6.0
    ports:
      - "4822:4822"
volumes:
  vantaged-data:
```

A bind mount (`./data:/data`) is fine too. The image runs as UID 65532, so that host directory must be writable by that user:

```bash
mkdir -p data
sudo chown 65532:65532 data
```

A named volume does this for you. If you bind-mount a directory owned by your login user, SQLite fails with `unable to open database file (14)`.

Images are tagged with the release version (`ghcr.io/jdbnet/vantaged:1.2.3`) and `latest`. Architectures: `linux/amd64` and `linux/arm64`.

## Sync desktop with vantaged

On vantaged, create an API key with the `sync` scope. In the desktop app Settings, paste the vantaged URL and that API key. Hosts, folders, tags, identities, snippets, and known hosts replicate both ways (last write wins).
