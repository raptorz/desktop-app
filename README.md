# Gemsnote Desktop

Gemsnote（珠玑笔记）桌面客户端，基于 Go + Wails v2。离线优先：数据存储在本地 SQLite，通过 USN 增量同步与 gemsnote 服务器保持一致；UI 复用 [gemsnote](https://github.com/gemsnote/gemsnote) 主仓库的 Vue Web 前端。

> 本项目基于 Leanote Electron 桌面端改写。

## 架构

```
Vue SPA（主仓库 frontend/ 构建产物，embed 进二进制）
   │  fetch 相对路径（/api2/*）
   ▼
webapi.Handler  ←  本地 API 兼容层（响应契约与 Revel 端 WebController 一致）
   │                    │
   │ 本地读写            │ 服务器专属功能代理
   ▼                    ▼
SQLite (db/)      gemsnote 服务器（共享/分组/管理/邮箱，cookie 会话）
   ▲
sync/ (USN 增量同步, /api2 开放 API + token)
```

- 服务器专属功能（共享协作、分组、管理后台、注册/找回密码、邮箱验证、头像）需要在线，由兼容层转发；
- 笔记、笔记本、标签、回收站、附件、图片、历史版本离线可用，改动经同步服务上行；
- 内容中的图片统一使用相对路径 `/api2/file/getImage?fileId=...`，由本地 Handler 直接从磁盘提供；
- 兼容层保留对历史数据中 `leanote://` 图片协议的识别与旧 `leanote` 数据目录的自动迁移。

## 构建

```bash
# 1. 构建前端（或直接运行 wails build，frontend:build 钩子会自动执行）
bash build-frontend.sh

# 2. 编译（需要各平台 webkit 依赖；Linux 需 libgtk-3-dev libwebkit2gtk-4.1-dev）
go build -o gemsnote .
# 或
wails build
```

## 开发

```bash
wails dev
```

## 数据

数据目录：`~/.config/gemsnote`（Windows: `%APPDATA%/gemsnote`，macOS: `~/Library/Application Support/gemsnote`）。首次启动会自动迁移旧 `leanote` 目录的数据。
