# Pearlnote Desktop

Pearlnote（珠玑笔记）桌面客户端，基于 Go + Wails v2。离线优先：数据存储在本地 SQLite，通过 USN 增量同步与 pearlnote 服务器保持一致；UI 复用 [pearlnote](https://github.com/pearlnote/pearlnote) 主仓库的 Vue Web 前端。

> 本仓库原为 Leanote Electron 桌面端，已完成向 Wails 的迁移并更名为 Pearlnote，Electron 实现已移除（详见 [docs/WAILS_MIGRATION_GUIDE.md](docs/WAILS_MIGRATION_GUIDE.md)）。

## 架构概览

```
Vue SPA（主仓库 frontend/ 构建产物，embed 进二进制）
   │  fetch 相对路径（/web/*, /note/*, /attach/* ...）
   ▼
webapi 兼容层（本地 API，响应契约与服务器端 WebController 一致）
   ├─ 本地读写 → SQLite + 本地文件（离线可用）
   └─ 服务器专属功能（共享/分组/管理/注册/邮箱）→ pearlnote 服务器
   ▲
USN 同步服务（/api 开放 API + token，菜单或自动触发）
```

详细说明见 [leanote-wails/leanote/README.md](leanote-wails/leanote/README.md)。

## 开发与构建

```bash
cd leanote-wails/leanote

bash build-frontend.sh   # 构建共享 Vue 前端并同步到 embed 目录
go build -o pearlnote .  # 或 wails build
wails dev                # 开发模式
```

Linux 编译依赖：`libgtk-3-dev`、`libwebkit2gtk-4.1-dev`。

## 数据

数据目录：`~/.config/pearlnote`（Windows: `%APPDATA%/pearlnote`，macOS: `~/Library/Application Support/pearlnote`）。首次启动会自动迁移旧 `leanote` 目录的本地数据。

## LICENSE

[LICENSE](LICENSE)

```
LEANOTE - NOT JUST A NOTEPAD!

Copyright by the contributors.

This program is free software; you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation; either version 2 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

Leanote destop app is licensed under the GPL v2.
```
