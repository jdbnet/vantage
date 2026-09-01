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

A typical guacd container on the same machine as the desktop app. Bind-mount the desktop shared folder at the same path inside the container, because that is the path the desktop sends as the RDP drive:

```bash
docker run -d --name guacd -p 4822:4822 \
  -v "$HOME/.local/share/vantage/shared:$HOME/.local/share/vantage/shared" \
  guacamole/guacd:1.6.0
```

If you instead point the desktop at vantaged's guacd, you do not need a local guacd. The guest then uses vantaged's shared folder (see below).

## Optional: vantaged server

vantaged is the same app as a headless server with a browser UI. Use it when you want a shared inventory, or to open sessions from a browser. It is published as a Docker image, not a standalone binary.

```bash
docker run -d --name vantaged \
  -p 7687:7687 \
  -v vantaged-data:/data \
  ghcr.io/jdbnet/vantaged:latest
```

Then open `http://127.0.0.1:7687` and create the operator account. That image has no shell and no `chmod`. Do not `docker exec` into it for filesystem tools.

For RDP, run guacd on the same Docker network and set Settings **guacd address** to `guacd:4822`. The RDP drive is `/data/shared` inside both containers, so that folder must be mounted in **both**:

```yaml
services:
  vantaged:
    image: ghcr.io/jdbnet/vantaged:latest
    ports:
      - "7687:7687"
    volumes:
      - ./data:/data
  guacd:
    image: guacamole/guacd:1.6.0
    volumes:
      - ./data/shared:/data/shared
```

vantaged runs as UID 65532. guacamole/guacd runs as UID 1000. The host directory must be writable by vantaged, and the shared folder must be traversable and writable by both:

```bash
mkdir -p data/shared
sudo chown 65532:65532 data
chmod a+x data
chmod -R a+rwX data/shared
```

Do not chmod the whole `data` tree world-writable (that is the SQLite vault). Only `data/shared` needs `a+rwX`. The vantaged image cannot run `chmod`; do it on the host, or `docker exec -u 0 <guacd-container> chmod -R a+rwX /data/shared`.

A named volume for `/data` is fine instead of `./data`. Create `/data/shared` on first start, then fix permissions the same way. If you bind-mount a directory owned by your login user, SQLite fails with `unable to open database file (14)`.

Leave **Drive path inside guacd** blank when the mounts match (`/data/shared` in both). The `Download` folder inside the drive is created by guacd for browser file transfer and is not synced.

Images are tagged with the release version (`ghcr.io/jdbnet/vantaged:1.2.3`) and `latest`. Architectures: `linux/amd64` and `linux/arm64`.

## Sync desktop with vantaged

On vantaged, create an API key with the `sync` scope. In the desktop app Settings, paste the vantaged URL and that API key. Hosts, folders, tags, identities, snippets, and known hosts replicate both ways (last write wins).

Shared files sync into the desktop data folder:

- Linux: `~/.local/share/vantage/shared`
- Windows: `%LOCALAPPDATA%\vantage\shared`
- macOS: `~/Library/Application Support/vantage/shared`

There is no shared-folder setting on the desktop. If **guacd address** is this machine (`127.0.0.1`), RDP uses that local folder. If it is vantaged's guacd, RDP uses vantaged's `/data/shared`, so the guest matches the web UI.
