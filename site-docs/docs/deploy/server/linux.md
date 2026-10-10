# Linux / systemd 部署

## 一键安装（推荐）

```bash
# 构建后一键安装（创建 argus 用户/目录、复制二进制、生成默认配置、注册 systemd 服务）
sudo deploy/systemd/install.sh build/argus

# 卸载
sudo deploy/systemd/install.sh --uninstall
```

脚本自动完成：创建 `argus` 用户与 `/opt/argus/data` → 安装二进制到
`/usr/local/bin/argus` → 生成 `/etc/argus/config.yaml`（sqlite，数据目录
`/opt/argus/data`）→ 生成 `/etc/argus.env`（密钥占位，需修改）→ 注册并启动服务。

> 首次安装后**务必修改 `/etc/argus.env` 中的密钥**（`openssl rand -hex 32`）再重启：
> ```bash
# 编辑密钥
sudo vi /etc/argus.env
sudo systemctl restart argus
```

## 手动安装

```bash
sudo install -m 0755 build/argus /usr/local/bin/argus
sudo mkdir -p /etc/argus /opt/argus/data
sudo useradd -r -s /usr/sbin/nologin argus
sudo chown -R argus:argus /opt/argus
```

## systemd

`deploy/systemd/argus.service` 已提供，安装：

```bash
sudo cp deploy/systemd/argus.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now argus
```

服务安全加固：`NoNewPrivileges`、`PrivateTmp`；密钥经 `EnvironmentFile=/etc/argus.env` 注入。

## 配置文件

- 主配置：`/etc/argus/config.yaml`（用户 `argus` 可读，含密钥路径）
- 环境文件：`/etc/argus.env`（0600）：

```
ARGUS_JWT_SECRET=...
ARGUS_MASTER_KEY=...
```

## 日志

```bash
journalctl -u argus -f
```
