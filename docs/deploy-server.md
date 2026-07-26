# ocswitch 服务器版部署手册

> 2026-07-25 迁移至新服务器 `149.30.172.66:16256`。旧 HK 机 `68.64.183.15` 观察期后下线。

## 1 服务器

| 项 | 值 |
|---|---|
| SSH | `ssh -p 16256 root@149.30.172.66` |
| 公网 IP | `149.30.172.66` |
| OS | Debian 12 (bookworm) x86_64 |
| 域名 | `ocswitch.apale7.cn` |

## 2 部署路径

| 文件 | 路径 |
|---|---|
| 二进制 | `/usr/local/bin/ocswitch-server` |
| 配置文件 | `/root/.config/ocswitch/config.json` |
| 配置备份 | `/root/.config/ocswitch/config.json.bak.20260725` |
| 日志/数据库 | `/root/.config/ocswitch/traces.db*`（软件自动管理） |
| systemd 单元 | `/etc/systemd/system/ocswitch-server.service` |

## 3 端口

| 端口 | 绑定 | 用途 |
|---|---|---|
| 9982 | 0.0.0.0 | 模型代理 API（`/v1/*`） |
| 9983 | 127.0.0.1 | 管理后台（仅经 Caddy 反代访问） |

## 4 systemd 服务

```ini
[Unit]
Description=ocswitch web admin
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
Environment=HOME=/root
Environment=OCSWITCH_CONFIG=/root/.config/ocswitch/config.json
WorkingDirectory=/root
ExecStart=/usr/local/bin/ocswitch-server server --host 127.0.0.1 --port 9983
Restart=on-failure
RestartSec=2
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
```

> 虽然 `--host 127.0.0.1 --port 9983` 只指定了管理端口，但 `ocswitch server` 会同时启动模型代理。模型代理端口从配置文件 `server.port` 读取（当前 9982，绑定 `0.0.0.0`）。

常用命令：

```bash
systemctl restart ocswitch-server
systemctl status ocswitch-server
journalctl -u ocswitch-server -f
```

## 5 Caddy 反代

```caddy
ocswitch.apale7.cn {
	encode zstd gzip
	reverse_proxy /v1/* 127.0.0.1:9982
	reverse_proxy 127.0.0.1:9983
}
```

> `/v1/*` 转发到模型代理，其余请求转发到管理后台。管理端不直接暴露公网。

## 6 配置文件结构

`config.json` 关键字段：

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 9982,
    "api_key": "<模型代理 API key>",
    "routing": { "strategy": "circuit-breaker" }
  },
  "admin": {
    "host": "127.0.0.1",
    "port": 9983,
    "api_key": "<管理员 token>",
    "public_base_url": "https://ocswitch.apale7.cn"
  },
  "providers": [...],
  "aliases": [...],
  "request_rewrite_rules": [...],
  "provider_priority": [...],
  "auto_alias_enabled": true
}
```

- `server.api_key`：客户端连接模型代理时使用的 API key。
- `admin.api_key`：登录管理后台时使用的管理员 token。
- 修改配置后必须 `systemctl restart ocswitch-server`。

## 7 构建

在本机（Windows）交叉编译 Linux amd64：

```powershell
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
go build -trimpath -ldflags '-s -w' -o ocswitch-server ./cmd/ocswitch
```

## 8 部署

```bash
# 上传新二进制
scp -P 16256 ocswitch-server root@149.30.172.66:/tmp/ocswitch-server

# 替换并重启
ssh -p 16256 root@149.30.172.66 "
  install -m 0755 /tmp/ocswitch-server /usr/local/bin/ocswitch-server &&
  systemctl restart ocswitch-server &&
  rm /tmp/ocswitch-server
"

# 验证
curl -fsS https://ocswitch.apale7.cn/
curl -fsS -H "Authorization: Bearer <server.api_key>" https://ocswitch.apale7.cn/v1/models
```

## 9 健康检查

```bash
# 管理后台（经 Caddy）
curl -fsS -o /dev/null -w "%{http_code}" https://ocswitch.apale7.cn/

# 模型 API（无 token 应返回 401）
curl -fsS -o /dev/null -w "%{http_code}" https://ocswitch.apale7.cn/v1/models

# 模型 API（带 token 应返回 200）
curl -fsS -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer <server.api_key>" \
  https://ocswitch.apale7.cn/v1/models

# 本地端口
ss -tlnp | grep -E ':(9982|9983)\b'
```

## 10 轮换凭据

### 管理员 token

```bash
# 停止服务
ssh -p 16256 root@149.30.172.66 "systemctl stop ocswitch-server"

# 备份配置
ssh -p 16256 root@149.30.172.66 "cp /root/.config/ocswitch/config.json /root/.config/ocswitch/config.json.bak.$(date +%Y%m%d%H%M%S)"

# 生成新 token 并更新（32 字节随机 base64url）
ssh -p 16256 root@149.30.172.66 "python3 -c \"
import json, secrets, base64
with open('/root/.config/ocswitch/config.json') as f:
    c = json.load(f)
c['admin']['api_key'] = base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip('=')
with open('/root/.config/ocswitch/config.json','w') as f:
    json.dump(c, f, indent=2, ensure_ascii=False)
\""

# 重启
ssh -p 16256 root@149.30.172.66 "systemctl start ocswitch-server"

# 查询新 token
ssh -p 16256 root@149.30.172.66 "python3 -c \"import json; c=json.load(open('/root/.config/ocswitch/config.json')); print(c['admin']['api_key'])\""
```

### 模型 API key

将上述命令中的 `admin` 替换为 `server` 即可。

## 11 约束与注意事项

- 管理端只监听 127.0.0.1，禁止直接暴露公网。
- Provider API key 存储在配置文件中明文；文件权限为 root 可读。
- Provider 应配置 IP 白名单（当前仅允许 `149.30.172.66`）。
- 旧 traces 数据不迁移；新服务器自行创建全新 `traces.db`。
- `ocswitch server` 命令行参数 `--host`/`--port` 仅控制管理端；模型代理端口从配置文件读取。