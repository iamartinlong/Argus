# Docker 部署

多阶段构建镜像（约 60MB）：node 前端构建 → Go 编译 → alpine 运行。

## 快速开始（一键脚本）

```bash
cd deploy/docker
./up.sh      # 自动生成 .env（随机密钥）+ 构建 + 启动
./down.sh    # 停止（数据保留）
./logs.sh    # 跟踪日志
```

## 手动启动

```bash
cd deploy/docker
# 准备 .env（或直接 export）
echo "ARGUS_JWT_SECRET=$(openssl rand -base64 48)" > .env
echo "ARGUS_MASTER_KEY=$(openssl rand -base64 32)" >> .env

docker compose up -d --build
```

访问 `http://localhost:8443`。

## 数据与配置

- 数据卷：`argus-data`（named volume，SQLite 数据库持久化，`docker compose down` 后保留）
- 配置：镜像内置 `/etc/argus/config.yaml`（listen `:8443`、sqlite `/data/argus.db`）
- 密钥：经 `.env` 注入环境变量（`ARGUS_JWT_SECRET` / `ARGUS_MASTER_KEY`）
- 首次使用需初始化管理员：

```bash
docker compose exec argus argus init-admin --username admin --password "..."
```

## 镜像内行为

- 非 root 用户运行（uid 1000）
- 包含 `openssh-client`（SSH 部署目标需要）
- 健康检查：`/healthz`

## 无 UI 构建（CLI-only）

```bash
docker build -f deploy/docker/Dockerfile --build-arg NOUI=1 .
```
