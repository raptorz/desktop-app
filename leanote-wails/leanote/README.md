# Pearlnote Desktop (Wails)

Go + Wails v2 桌面客户端。离线优先：本地 SQLite 存储笔记数据，后台通过 USN 增量同步与 pearlnote 服务器保持一致。UI 复用仓库根目录 `frontend/` 的 Vue SPA。

## 架构

```
Vue SPA (frontend/ 构建产物, embed 进二进制)
   │  fetch 相对路径 (/web/*, /note/*, /attach/* ...)
   ▼
webapi.Handler  ←  本地 API 兼容层（响应契约与 Revel 端 WebController 一致）
   │                    │
   │ 本地读写            │ 服务器专属功能代理
   ▼                    ▼
SQLite (db/)      pearlnote 服务器（共享/分组/管理/邮箱，cookie 会话）
   ▲
sync/ (USN 增量同步, /api 开放 API + token)
```

- 服务器专属功能（共享协作、分组、管理后台、注册/找回密码、邮箱验证、头像）需要在线，由兼容层转发；
- 笔记、笔记本、标签、回收站、附件、图片、历史版本离线可用，改动经同步服务上行；
- 内容中的图片统一使用相对路径 `/api/file/getImage?fileId=...`，由本地 Handler 直接从磁盘提供。

## 构建

```bash
# 1. 构建前端（或直接运行 wails build，frontend:build 钩子会自动执行）
bash build-frontend.sh

# 2. 编译（需要各平台 webkit 依赖；Linux 需 libgtk-3-dev libwebkit2gtk-4.1-dev）
go build -o pearlnote .
# 或
wails build
```

## 开发

```bash
wails dev
```

## 数据

数据目录：`~/.config/pearlnote`（Windows: `%APPDATA%/pearlnote`，macOS: `~/Library/Application Support/pearlnote`）。首次启动会自动迁移旧 `leanote` 目录的数据。
