# ocswitch Server Image Update Runbook

## Current Deployment

- Server: `<ssh-user>@<server-host>`
- Domain: `<ocswitch-domain>`
- Container: `ocswitch-server`
- Image: `ocswitch-server:latest`
- App root: `<deploy-root>`
- Mounted config: `<deploy-root>/config/config.json` -> container `/config/config.json`
- Runtime Dockerfile: `<deploy-root>/Dockerfile.runtime`
- Runtime binary on server: `<deploy-root>/ocswitch-server`
- Caddy routes:
  - `/v1/*` -> `127.0.0.1:9982`
  - other paths -> `127.0.0.1:9983`

## Important Secrets

- Provider API keys live in `<deploy-root>/config/config.json`.
- Admin token lives in `<deploy-root>/config/config.json` at `admin.api_key`.
- Proxy API key lives in `<deploy-root>/config/config.json` at `server.api_key`.
- Do not print config content in logs or chat.
- Preserve `<deploy-root>/config/config.json` during updates.

## Fast Update Path

Use this when local source has the desired code and frontend changes.

1. Build frontend locally so Go embeds current Web assets.

```powershell
rtk npm run build
```

2. Cross-compile Linux amd64 binary locally.

```powershell
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
rtk go build -trimpath -ldflags "-s -w" -o "$env:TEMP\ocswitch-server" ./cmd/ocswitch
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```

3. Restore local embed placeholder if `npm run build` removed it and you do not intend to commit `frontend/dist`.

```powershell
Set-Content -Path "frontend/dist/embed-placeholder.txt" -Value "placeholder for Go embed before Wails builds frontend assets"
```

4. Upload binary.

```powershell
scp "$env:TEMP\ocswitch-server" <ssh-user>@<server-host>:<deploy-root>/ocswitch-server
```

5. Rebuild small runtime image on server and restart container.

```bash
ssh <ssh-user>@<server-host> "set -e; chmod +x <deploy-root>/ocswitch-server; cp /etc/ssl/certs/ca-certificates.crt <deploy-root>/ca-certificates.crt; cd <deploy-root>; docker build -f Dockerfile.runtime -t ocswitch-server:latest .; docker rm -f ocswitch-server >/dev/null 2>&1 || true; docker run -d --name ocswitch-server --restart unless-stopped -p 127.0.0.1:9982:9982 -p 127.0.0.1:9983:9983 -v <deploy-root>/config:/config ocswitch-server:latest server --host 0.0.0.0 --config /config/config.json"
```

6. Verify container and local reverse-proxy targets.

```bash
ssh <ssh-user>@<server-host> "docker ps --filter name=ocswitch-server --format '{{.Names}} {{.Image}} {{.Ports}}'; docker logs --tail 30 ocswitch-server; curl -sS -o /tmp/ocswitch-admin.html -w '%{http_code} %{content_type} %{size_download}\n' http://127.0.0.1:9983/; curl -sS http://127.0.0.1:9982/healthz"
```

Expected:

- Container status running.
- Admin root returns `401` if no token is provided.
- Health returns `{"status":"ok"}`.

7. Verify public domain after DNS/TLS is ready.

```bash
ssh <ssh-user>@<server-host> "curl -sS -o /tmp/ocswitch-public.html -w '%{http_code} %{content_type} %{size_download}\n' https://<ocswitch-domain>/; curl -sS https://<ocswitch-domain>/healthz"
```

Expected:

- Admin root returns `401` without token.
- `/healthz` returns `{"status":"ok"}`.

## Full Source Upload Path

Use this when server should build from source, but note remote Docker Hub pulls may fail due network/cert issues. The current deployment avoided this by uploading a locally built static binary.

1. Create source archive excluding local caches and generated dist.

```powershell
tar --exclude=.git --exclude=.trellis --exclude=.opencode --exclude=build --exclude=frontend/node_modules --exclude=frontend/dist --exclude=frontend/wailsjs -cf "$env:TEMP\ocswitch-src.tar" .
```

2. Upload and extract.

```powershell
scp "$env:TEMP\ocswitch-src.tar" <ssh-user>@<server-host>:<deploy-root>/src/ocswitch-src.tar
ssh <ssh-user>@<server-host> "rm -rf <deploy-root>/src/buildctx; mkdir -p <deploy-root>/src/buildctx; tar -xf <deploy-root>/src/ocswitch-src.tar -C <deploy-root>/src/buildctx"
```

3. Build with `Dockerfile.server` only if remote base image pulls work.

```bash
ssh <ssh-user>@<server-host> "cd <deploy-root>/src/buildctx && docker build -f Dockerfile.server -t ocswitch-server:latest ."
```

## Caddy

Caddy config lives at `/etc/caddy/Caddyfile`.

Expected block:

```caddyfile
<ocswitch-domain> {
    encode zstd gzip
    reverse_proxy /v1/* 127.0.0.1:9982
    reverse_proxy 127.0.0.1:9983
}
```

Reload after changes:

```bash
ssh <ssh-user>@<server-host> "caddy validate --config /etc/caddy/Caddyfile && systemctl reload caddy && systemctl is-active caddy"
```

## DNS/TLS Check

```bash
ssh <ssh-user>@<server-host> "getent ahosts <ocswitch-domain>; curl -sS https://api.ipify.org"
```

`<ocswitch-domain>` should resolve to the server public IP. If it does not, Caddy HTTPS certificate issuance will fail until DNS propagates.

## Rollback

If new container fails, use previous uploaded binary if preserved, or rebuild from previous git checkout.

Minimal rollback if old image still exists under a tag:

```bash
ssh <ssh-user>@<server-host> "docker rm -f ocswitch-server; docker run -d --name ocswitch-server --restart unless-stopped -p 127.0.0.1:9982:9982 -p 127.0.0.1:9983:9983 -v <deploy-root>/config:/config <old-image-tag> server --host 0.0.0.0 --config /config/config.json"
```

Recommended improvement: before rebuilding, tag current image:

```bash
ssh <ssh-user>@<server-host> "docker tag ocswitch-server:latest ocswitch-server:rollback-$(date +%Y%m%d%H%M%S)"
```
