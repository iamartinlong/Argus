# ---------- 前端构建 ----------
FROM node:24-alpine AS frontend
# 国内网络构建慢可取消下一行注释（使用淘宝镜像源）
# RUN npm config set registry https://registry.npmmirror.com
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- Go 构建 ----------
FROM golang:1.26-alpine AS backend
# 国内网络构建慢可改 ENV GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=https://proxy.golang.org,direct
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 嵌入前端产物（webassets/dist 由 frontend 阶段产出）
COPY --from=frontend /app/webassets/dist ./webassets/dist
RUN CGO_ENABLED=0 go build -o /argus ./cmd/argus

# ---------- 运行镜像 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates openssh-client && \
    adduser -D -H -u 1000 argus
COPY --from=backend /argus /usr/local/bin/argus
COPY deploy/docker/config.yaml /etc/argus/config.yaml
RUN mkdir -p /data && chown -R argus:argus /data /etc/argus
VOLUME /data
EXPOSE 8443
USER argus
ENTRYPOINT ["argus"]
CMD ["serve", "--config", "/etc/argus/config.yaml"]
