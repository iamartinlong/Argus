---
layout: home

hero:
  name: "Argus"
  text: "永不眨眼的 SSL 证书守护者"
  tagline: 源自希腊神话的百眼巨人 —— 永远盯着每一张证书，到期自动续、签发即部署
  actions:
    - theme: brand
      text: 快速开始
      link: /guide/quickstart
    - theme: alt
      text: 查看架构
      link: /architecture/overview

features:
  - icon: 🛡️
    title: 多渠道签发
    details: ACME（Let's Encrypt 等）、阿里云、腾讯云免费证书统一管理，HTTP-01 / DNS-01 验证
  - icon: 🔄
    title: 全自动续期
    details: 到期前自动续期、失败重试、多级到期提醒（30/14/7/1 天）
  - icon: 🚀
    title: 多通道部署
    details: SSH、Agent（穿透 NAT）、K8s Secret、Webhook、Windows IIS（PFX 导入+绑定）
  - icon: 📢
    title: 通知中心
    details: SMTP、Webhook、企业微信/钉钉/飞书机器人，失败自动重试
  - icon: 💻
    title: Web + CLI 双前端
    details: React 管理后台 + cobra CLI（本地进程内 / 远程 API Token 模式）
  - icon: 📦
    title: 单二进制交付
    details: 前端产物内嵌，Windows 服务 / systemd / Docker 一键部署
---
