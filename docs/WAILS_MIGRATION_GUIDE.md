# Leanote Desktop App: Electron → Wails 迁移指南

> 版本: 1.0  
> 日期: 2026-04-18  
> 状态: 已完成迁移  
> 技术栈: Wails v2 + Go 1.22

---

## 目录

1. [概述](#1-概述)
2. [现状分析](#2-现状分析)
3. [目标架构](#3-目标架构)
4. [数据库迁移](#4-数据库迁移)
5. [同步服务迁移](#5-同步服务迁移)
6. [测试策略](#6-测试策略)
7. [迁移执行计划](#7-迁移执行计划)
8. [风险管理](#8-风险管理)

---

## 1. 概述

### 1.1 迁移目标

将现有的 Electron 桌面应用迁移至 Go + Wails 技术栈，实现：

- 安装包体积从 ~150MB 降至 ~15-25MB
- 内存占用从 ~200-400MB 降至 ~50-100MB
- 提升启动速度和运行性能
- 简化部署和跨平台编译（Go 静态编译）
- 保持现有前端代码基本不变

### 1.2 迁移范围

| 组件 | 迁移方式 | 工作量 |
|------|----------|--------|
| 前端 UI | 直接复用 | 低 |
| 数据库层 | 完全重写 | 高 |
| 同步服务 | 完全重写 | 高 |
| API 客户端 | 完全重写 | 中 |
| IPC 通信 | 重构适配 | 中 |
| 文件操作 | Go 标准库 | 低 |
| PDF 导出 | 重写 | 中 |
| 自定义协议 | Wails AssetServer | 低 |

### 1.3 技术选型确认

| 组件 | 选型 | 说明 |
|------|------|------|
| 框架 | Wails v2 | 稳定版 |
| 语言 | Go 1.22 | 用户指定 |
| 数据库驱动 | modernc.org/sqlite | 纯 Go，方便跨平台编译 |
| HTTP 客户端 | github.com/go-resty/resty | 简洁的 API |
| 日志 | github.com/sirupsen/logrus | 结构化日志 |

### 1.4 预计工期

- **总工期**: 4-6 周
- **核心开发**: 3-4 周
- **测试与优化**: 1-2 周

---

## 2. 现状分析

### 2.1 项目结构

```
desktop-app/
├── main.js              # Electron 主进程入口 (317行)
├── note.html            # 主界面
├── login.html           # 登录界面
├── src/                 # 后端逻辑 (Node.js)
│   ├── sync.js          # 同步服务 (1226行) ⚠️ 核心
│   ├── note.js          # 笔记服务 (1905行) ⚠️ 核心
│   ├── notebook.js      # 笔记本服务 (513行)
│   ├── user.js          # 用户服务 (783行)
│   ├── api.js           # API 客户端 (796行)
│   ├── db.js            # 数据库初始化 (103行)
│   ├── db_main.js       # 主进程数据库 (96行)
│   ├── tag.js           # 标签服务
│   ├── file.js          # 文件操作
│   └── common.js        # 工具函数 (327行)
├── public/              # 前端资源
│   ├── js/app/          # 应用 JS
│   ├── tinymce/         # 富文本编辑器
│   ├── plugins/         # 插件系统 (14个插件)
│   └── themes/          # 主题 (LESS)
└── tests/               # 测试文件
```

### 2.2 技术栈对比

| 层级 | 当前技术 | 迁移目标 |
|------|----------|----------|
| 运行时 | Electron v12.0.2 | Wails v2 |
| 后端语言 | Node.js | Go 1.22 |
| 数据库 | NeDB (嵌入式 MongoDB) | SQLite (纯 Go 驱动) |
| 前端框架 | jQuery + Bootstrap | 保持不变 |
| 编辑器 | TinyMCE + Ace | 保持不变 |
| HTTP 客户端 | needle | resty v2 |
| 并发模型 | async.js | goroutine |
| IPC | ipcMain/ipcRenderer | Wails Bindings |

### 2.3 数据模型

#### NeDB 集合结构

| 集合 | 说明 | 关键字段 |
|------|------|----------|
| users | 用户信息 | UserId, Token, LastSyncUsn |
| notebooks | 笔记本 | NotebookId, ServerNotebookId, Usn, IsDirty |
| notes | 笔记 | NoteId, ServerNoteId, Usn, IsDirty, Content |
| tags | 标签 | Tag, Usn, IsDirty |
| noteHistories | 笔记历史 | NoteId, Histories[] |
| attachs | 附件 | FileId, ServerFileId, Path |
| images | 图片 | FileId, ServerFileId, Path |

#### 核心同步字段说明

```
Usn (Update Sequence Number):
- 服务器数据版本号，每次变更 +1
- 用于增量同步判断

IsDirty:
- 本地是否已修改，待同步到服务器

LocalIsNew:
- 本地新建，尚未同步到服务器

LocalIsDelete:
- 本地删除，尚未同步到服务器

ServerNoteId / ServerNotebookId:
- 服务器端 ID 映射
- 本地 ID 与服务器 ID 可能不同

InitSync:
- 刚从服务器同步，内容/附件尚未下载
```

---

## 3. 目标架构

### 3.1 整体架构图

```
┌─────────────────────────────────────────────────────────────┐
│                    Frontend (WebView)                        │
│                                                             │
│   jQuery + TinyMCE + Ace Editor (保持不变)                   │
│                                                             │
│   改动点:                                                    │
│   • ipcRenderer.send() → wails.go 调用                       │
│   • @electron/remote → Wails API                            │
│   • require('fs') → Go 文件操作绑定                          │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          │ Wails Bindings (自动生成)
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                     Go Backend (Wails)                       │
│                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │   Sync      │  │    API      │  │   Database  │         │
│  │   Service   │  │   Client    │  │   (SQLite)  │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
│                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │  Asset      │  │    File     │  │     PDF     │         │
│  │  Handler    │  │   System    │  │   Export    │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 Go 项目结构

```
.
├── main.go                    # Wails 入口
├── app.go                     # 应用绑定 (Wails 结构体)
├── wails.json                 # Wails 配置
├── go.mod                     # Go 模块定义
├── go.sum
│
├── models/                    # 数据模型
│   ├── user.go
│   ├── notebook.go
│   ├── note.go
│   ├── tag.go
│   ├── attach.go
│   └── sync.go                # 同步相关类型
│
├── db/                        # 数据库层
│   ├── db.go                  # 连接管理
│   ├── user_repo.go
│   ├── notebook_repo.go
│   ├── note_repo.go
│   ├── tag_repo.go
│   ├── attach_repo.go
│   └── migrations.sql         # 建表脚本
│
├── sync/                      # 同步服务
│   ├── sync.go                # 同步服务主类
│   ├── pull.go                # Pull 逻辑
│   ├── push.go                # Push 逻辑
│   ├── conflict.go            # 冲突处理
│   └── progress.go            # 进度管理
│
├── api/                       # API 客户端
│   ├── client.go              # HTTP 客户端封装
│   ├── auth.go                # 认证 API
│   ├── notebook.go            # 笔记本 API
│   ├── note.go                # 笔记 API
│   └── file.go                # 文件/图片 API
│
├── assets/                    # 前端资源 (构建后)
│   └── ...
│
├── frontend/                  # 前端源码
│   ├── index.html
│   ├── src/
│   └── ...
│
└── utils/                     # 工具函数
    ├── objectid.go            # ID 生成
    ├── datetime.go            # 时间处理
    └── crypto.go              # MD5 等
```

### 3.3 Go 依赖选型

```go
// go.mod
module leanote-desktop

go 1.22

require (
    // Wails 核心
    github.com/wailsapp/wails/v2 v2.8.0
    
    // 数据库 (纯 Go SQLite)
    modernc.org/sqlite v1.29.1
    
    // HTTP 客户端
    github.com/go-resty/resty/v2 v2.12.0
    
    // JSON
    encoding/json  // 标准库
    
    // 日志
    github.com/sirupsen/logrus v1.9.3
    
    // UUID
    github.com/google/uuid v1.6.0
    
    // 配置
    github.com/spf13/viper v1.18.2
)

// 开发依赖
require (
    github.com/wailsapp/wails/v2/cmd/wails v2.8.0 // indirect
)
```

### 3.4 Wails 应用结构

```go
// main.go
package main

import (
    "embed"
    
    "github.com/wailsapp/wails/v2"
    "github.com/wailsapp/wails/v2/pkg/options"
    "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
    // 创建应用实例
    app := NewApp()
    
    err := wails.Run(&options.App{
        Title:  "Leanote",
        Width:  1050,
        Height: 595,
        AssetServer: &assetserver.Options{
            Assets: assets,
            // 自定义协议处理
            Handler: NewLeanoteHandler(),
        },
        OnStartup:  app.startup,
        OnShutdown: app.shutdown,
        Bind: []interface{}{
            app,
        },
    })
    
    if err != nil {
        println("Error:", err.Error())
    }
}

// app.go
package main

import (
    "context"
    
    "leanote-desktop/db"
    "leanote-desktop/sync"
    "leanote-desktop/api"
)

type App struct {
    ctx     context.Context
    db      *db.Database
    sync    *sync.SyncService
    api     *api.Client
}

func NewApp() *App {
    return &App{}
}

func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
    a.db = db.New()
    a.api = api.NewClient()
    a.sync = sync.NewSyncService(a.db, a.api)
}

// ==================== 用户相关 ====================

func (a *App) Login(username, password, host string) (*User, error) {
    return a.api.Auth(username, password, host)
}

func (a *App) GetCurrentUser() (*User, error) {
    return a.db.GetActiveUser()
}

// ==================== 笔记本相关 ====================

func (a *App) GetNotebooks() ([]*Notebook, error) {
    return a.db.GetNotebooks()
}

func (a *App) AddNotebook(title, parentID string) (*Notebook, error) {
    return a.db.AddNotebook(title, parentID)
}

// ==================== 笔记相关 ====================

func (a *App) GetNotes(notebookID string) ([]*Note, error) {
    return a.db.GetNotes(notebookID)
}

func (a *App) GetNoteContent(noteID string) (*Note, error) {
    return a.db.GetNoteContent(noteID)
}

func (a *App) UpdateNote(note *Note) error {
    return a.db.UpdateNote(note)
}

// ==================== 同步相关 ====================

func (a *App) FullSync() (*SyncInfo, error) {
    return a.sync.FullSync()
}

func (a *App) IncrSync() (*SyncInfo, error) {
    return a.sync.IncrSync()
}

// ==================== 文件相关 ====================

func (a *App) GetImage(fileID string) (string, error) {
    // 返回 leanote:// 协议 URL 或本地路径
    return a.db.GetImagePath(fileID)
}
```

---

## 4. 数据库迁移

### 4.1 NeDB → SQLite 表结构映射

#### 4.1.1 用户表 (users)

```sql
CREATE TABLE users (
    _id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    email TEXT,
    pwd TEXT,                        -- MD5 加密
    token TEXT,
    host TEXT,                       -- 服务器地址
    last_sync_usn INTEGER DEFAULT -1,
    last_sync_time INTEGER,
    notebook_usn INTEGER DEFAULT -1,
    note_usn INTEGER DEFAULT -1,
    tag_usn INTEGER DEFAULT -1,
    is_active INTEGER DEFAULT 0,
    is_local INTEGER DEFAULT 0,      -- 是否本地账户
    has_db INTEGER DEFAULT 0,
    state TEXT,                      -- JSON: UI状态
    created_time INTEGER,
    last_login_time INTEGER
);

CREATE INDEX idx_users_active ON users(is_active);
```

#### 4.1.2 笔记本表 (notebooks)

```sql
CREATE TABLE notebooks (
    _id TEXT PRIMARY KEY,
    notebook_id TEXT NOT NULL,
    server_notebook_id TEXT,         -- 服务器ID映射
    user_id TEXT NOT NULL,
    parent_notebook_id TEXT,
    title TEXT NOT NULL,
    seq INTEGER DEFAULT 0,           -- 排序
    number_notes INTEGER DEFAULT 0,
    url_title TEXT,
    is_blog INTEGER DEFAULT 0,
    is_trash INTEGER DEFAULT 0,
    usn INTEGER DEFAULT 0,
    is_dirty INTEGER DEFAULT 0,      -- 已修改待同步
    local_is_new INTEGER DEFAULT 0,  -- 本地新建
    local_is_delete INTEGER DEFAULT 0,
    created_time INTEGER,
    updated_time INTEGER,
    
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX idx_notebooks_user ON notebooks(user_id);
CREATE INDEX idx_notebooks_server_id ON notebooks(server_notebook_id);
CREATE INDEX idx_notebooks_dirty ON notebooks(user_id, is_dirty);
CREATE INDEX idx_notebooks_parent ON notebooks(parent_notebook_id);
```

#### 4.1.3 笔记表

```sql
CREATE TABLE notes (
    _id TEXT PRIMARY KEY,
    note_id TEXT NOT NULL UNIQUE,
    server_note_id TEXT,             -- 服务器ID映射
    notebook_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT,
    content TEXT,
    desc TEXT,
    abstract TEXT,                   -- Markdown 摘要
    img_src TEXT,
    tags TEXT,                       -- JSON array: ["tag1", "tag2"]
    is_markdown INTEGER DEFAULT 0,
    is_trash INTEGER DEFAULT 0,
    is_blog INTEGER DEFAULT 0,
    is_star INTEGER DEFAULT 0,
    usn INTEGER DEFAULT 0,
    is_dirty INTEGER DEFAULT 0,
    content_is_dirty INTEGER DEFAULT 0,
    local_is_new INTEGER DEFAULT 0,
    local_is_delete INTEGER DEFAULT 0,
    init_sync INTEGER DEFAULT 0,     -- 刚同步，内容待加载
    conflict_note_id TEXT,           -- 冲突笔记ID
    conflict_time INTEGER,
    conflict_fixed INTEGER DEFAULT 0,
    err TEXT,                        -- 同步错误信息
    created_time INTEGER,
    updated_time INTEGER,
    
    FOREIGN KEY (notebook_id) REFERENCES notebooks(notebook_id),
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX idx_notes_user ON notes(user_id);
CREATE INDEX idx_notes_notebook ON notes(notebook_id);
CREATE INDEX idx_notes_server_id ON notes(server_note_id);
CREATE INDEX idx_notes_dirty ON notes(user_id, is_dirty);
CREATE INDEX idx_notes_trash ON notes(user_id, is_trash);
CREATE INDEX idx_notes_star ON notes(user_id, is_star);

-- 全文搜索虚拟表
CREATE VIRTUAL TABLE notes_fts USING fts5(
    title,
    content,
    content='notes',
    content_rowid='rowid'
);

-- 触发器：保持 FTS 索引同步
CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts(rowid, title, content) 
    VALUES (new.rowid, new.title, new.content);
END;

CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, content) 
    VALUES('delete', old.rowid, old.title, old.content);
END;

CREATE TRIGGER notes_au AFTER UPDATE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, content) 
    VALUES('delete', old.rowid, old.title, old.content);
    INSERT INTO notes_fts(rowid, title, content) 
    VALUES (new.rowid, new.title, new.content);
END;
```

#### 4.1.4 标签表

```sql
CREATE TABLE tags (
    _id TEXT PRIMARY KEY,
    tag TEXT NOT NULL,
    user_id TEXT NOT NULL,
    count INTEGER DEFAULT 0,
    usn INTEGER DEFAULT 0,
    is_dirty INTEGER DEFAULT 0,
    local_is_delete INTEGER DEFAULT 0,
    created_time INTEGER,
    updated_time INTEGER,
    
    FOREIGN KEY (user_id) REFERENCES users(_id),
    UNIQUE(user_id, tag)
);

CREATE INDEX idx_tags_user ON tags(user_id);
CREATE INDEX idx_tags_dirty ON tags(user_id, is_dirty);
```

#### 4.1.5 笔记历史表 (note_histories)

```sql
CREATE TABLE note_histories (
    _id INTEGER PRIMARY KEY AUTOINCREMENT,
    note_id TEXT NOT NULL,
    content TEXT,
    updated_time INTEGER,
    
    FOREIGN KEY (note_id) REFERENCES notes(note_id)
);

CREATE INDEX idx_histories_note ON note_histories(note_id);
```

#### 4.1.6 附件表

```sql
CREATE TABLE attachs (
    _id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    server_file_id TEXT,
    note_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT,
    type TEXT,                       -- 文件类型
    path TEXT,                       -- 本地路径
    is_attach INTEGER DEFAULT 1,
    is_dirty INTEGER DEFAULT 0,
    created_time INTEGER,
    
    FOREIGN KEY (note_id) REFERENCES notes(note_id),
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX idx_attachs_note ON attachs(note_id);
CREATE INDEX idx_attachs_server_id ON attachs(server_file_id);
```

#### 4.1.7 图片表

```sql
CREATE TABLE images (
    _id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL UNIQUE,
    server_file_id TEXT,
    user_id TEXT NOT NULL,
    path TEXT,
    is_dirty INTEGER DEFAULT 0,
    created_time INTEGER,
    
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX idx_images_server_id ON images(server_file_id);
```

#### 4.1.8 全局配置表

```sql
CREATE TABLE config (
    key TEXT PRIMARY KEY,
    value TEXT                       -- JSON
);
```

### 4.2 数据迁移流程

```
┌─────────────────────────────────────────────────────────────┐
│                      数据迁移流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  1. 检测 NeDB 文件                                          │
│     └─ 扫描 nedb/ 目录下的 *.db 文件                         │
│                                                             │
│  2. 解析 NeDB 格式                                          │
│     └─ 每行一个 JSON 对象                                    │
│                                                             │
│  3. 类型转换                                                │
│     ├─ Date → Unix Timestamp (int64)                        │
│     ├─ Boolean → int (0/1)                                  │
│     ├─ Array → JSON string                                  │
│     └─ null → NULL                                          │
│                                                             │
│  4. 写入 SQLite                                             │
│     ├─ 开启事务                                              │
│     ├─ 批量插入                                              │
│     └─ 提交事务                                              │
│                                                             │
│  5. 验证数据完整性                                          │
│     ├─ 记录数量对比                                          │
│     ├─ 关键字段抽样检查                                      │
│     └─ 外键完整性检查                                        │
│                                                             │
│  6. 建立索引                                                │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 4.3 迁移代码设计 (Go)

```go
// migration/migrator.go
package migration

import (
    "bufio"
    "encoding/json"
    "os"
    "path/filepath"
    
    "leanote-desktop/db"
    "leanote-desktop/models"
)

type Migrator struct {
    sourcePath string
    targetDB   *db.Database
}

type MigrationReport struct {
    Users     int
    Notebooks int
    Notes     int
    Tags      int
    Errors    []error
}

func NewMigrator(sourcePath string, targetDB *db.Database) *Migrator {
    return &Migrator{
        sourcePath: sourcePath,
        targetDB:   targetDB,
    }
}

func (m *Migrator) Migrate() (*MigrationReport, error) {
    report := &MigrationReport{}
    
    // 开启事务
    tx, err := m.targetDB.Begin()
    if err != nil {
        return nil, err
    }
    defer tx.Rollback()
    
    // 1. 迁移用户
    users, err := m.parseNeDB[models.User]("users.db")
    if err != nil {
        return nil, err
    }
    for _, user := range users {
        if err := m.targetDB.InsertUserTx(tx, &user); err != nil {
            report.Errors = append(report.Errors, err)
        } else {
            report.Users++
        }
    }
    
    // 2. 迁移笔记本
    notebooks, err := m.parseNeDB[models.Notebook]("notebooks.db")
    if err != nil {
        return nil, err
    }
    for _, nb := range notebooks {
        if err := m.targetDB.InsertNotebookTx(tx, &nb); err != nil {
            report.Errors = append(report.Errors, err)
        } else {
            report.Notebooks++
        }
    }
    
    // 3. 迁移笔记
    notes, err := m.parseNeDB[models.Note]("notes.db")
    if err != nil {
        return nil, err
    }
    for _, note := range notes {
        if err := m.targetDB.InsertNoteTx(tx, &note); err != nil {
            report.Errors = append(report.Errors, err)
        } else {
            report.Notes++
        }
    }
    
    // 4. 迁移标签
    tags, err := m.parseNeDB[models.Tag]("tags.db")
    if err != nil {
        return nil, err
    }
    for _, tag := range tags {
        if err := m.targetDB.InsertTagTx(tx, &tag); err != nil {
            report.Errors = append(report.Errors, err)
        } else {
            report.Tags++
        }
    }
    
    // 提交事务
    if err := tx.Commit(); err != nil {
        return nil, err
    }
    
    return report, nil
}

// 解析 NeDB 格式文件
func (m *Migrator) parseNeDB[T any](filename string) ([]T, error) {
    filePath := filepath.Join(m.sourcePath, filename)
    
    file, err := os.Open(filePath)
    if err != nil {
        return nil, err
    }
    defer file.Close()
    
    var records []T
    scanner := bufio.NewScanner(file)
    
    for scanner.Scan() {
        line := scanner.Text()
        if len(line) == 0 {
            continue
        }
        
        var record T
        if err := json.Unmarshal([]byte(line), &record); err != nil {
            continue // 跳过解析失败的行
        }
        records = append(records, record)
    }
    
    return records, scanner.Err()
}
```

---

## 5. 同步服务迁移

### 5.1 同步协议理解

#### 5.1.1 USN (Update Sequence Number) 机制

```
USN 是 Leanote 同步的核心：

服务器端:
- 每次数据变更，全局 USN + 1
- 每个对象有自己的 USN

客户端:
- 保存 LastSyncUsn (上次同步时的全局 USN)
- 增量同步时传递 afterUsn = LastSyncUsn
- 服务器返回所有 Usn > afterUsn 的变更
```

#### 5.1.2 同步类型

| 类型 | 触发条件 | 范围 |
|------|----------|------|
| 全量同步 | 首次登录、强制刷新 | 拉取所有数据 |
| 增量同步 | 定时触发、手动触发 | 只拉取变更 |

### 5.2 同步流程详解

#### 5.2.1 全量同步

```
FullSync()
    │
    ├─ 1. 初始化同步状态
    │   └─ 重置计数器、状态标志
    │
    ├─ 2. 获取本地同步状态
    │   └─ GetAllLastSyncState()
    │       → {lastUsn, notebookUsn, noteUsn, tagUsn}
    │
    ├─ 3. 获取服务器同步状态
    │   └─ API.GetLastSyncState()
    │       → {LastSyncUsn, LastSyncTime}
    │
    ├─ 4. Pull 阶段 (从服务器拉取)
    │   │
    │   ├─ SyncNotebook(afterUsn)
    │   │   ├─ GetSyncNotebooks(afterUsn, 200)
    │   │   ├─ 分块处理 (每块200条)
    │   │   ├─ 本地合并逻辑
    │   │   └─ 更新本地 NotebookUsn
    │   │
    │   ├─ SyncNote(afterUsn)
    │   │   ├─ GetSyncNotes(afterUsn, 200)
    │   │   ├─ 冲突检测与处理
    │   │   └─ 更新本地 NoteUsn
    │   │
    │   └─ SyncTag(afterUsn)
    │       ├─ GetSyncTags(afterUsn, 200)
    │       └─ 本地合并
    │
    ├─ 5. 更新同步状态
    │   └─ UpdateLastSyncState()
    │
    ├─ 6. Push 阶段 (发送本地变更)
    │   └─ SendChanges()
    │       ├─ SendNotebookChanges()
    │       ├─ SendNoteChanges()
    │       └─ SendTagChanges()
    │
    └─ 7. 处理冲突
        └─ FixConflicts()
```

#### 5.2.2 增量同步

```
IncrSync()
    │
    ├─ 1. 获取本地 LastSyncUsn
    │
    ├─ 2. 获取服务器 LastSyncUsn
    │
    ├─ 3. 比较是否需要 Pull
    │   │
    │   └─ if server.LastSyncUsn > local.LastSyncUsn:
    │       ├─ SyncNotebook(local.LastSyncUsn)
    │       ├─ SyncNote(local.LastSyncUsn)
    │       └─ SyncTag(local.LastSyncUsn)
    │
    ├─ 4. Push 本地变更
    │   └─ SendChanges()
    │
    └─ 5. 检查是否需要重新同步
        └─ if needIncrSyncAgain && retry < 5:
            → 递归调用 IncrSync()
```

### 5.3 冲突处理算法

#### 5.3.1 笔记冲突检测

```
Pull 阶段冲突检测 (sync.js:347-391):

当服务器笔记同步到本地时:

if (localNote.IsDirty) {
    // 本地有修改，可能冲突
    
    serverContent = GetNoteContentFromServer(serverNoteId)
    
    if (serverContent == localNote.Content) {
        // 内容相同 → 不冲突
        // 用服务器元数据更新本地
        UpdateNoteForce(serverNote, needReloadContent=false)
    } else {
        // 内容不同 → 真冲突
        // 记录冲突，由前端或后续处理
        conflicts.push({ server: serverNote, local: localNote })
    }
} else {
    // 本地未修改 → 直接用服务器数据
    UpdateNoteForce(serverNote)
}
```

#### 5.3.2 冲突解决方案

```
冲突处理流程 (note.js:1267-1382):

1. 检测到冲突
   └─ {server: serverNote, local: localNote}

2. 复制本地笔记
   ├─ 生成新的 NoteId
   ├─ 设置 ConflictNoteId = 原NoteId
   ├─ 设置 ConflictFixed = false
   └─ 保留本地内容和附件

3. 用服务器数据覆盖本地原笔记
   ├─ ServerNoteId = server.NoteId
   ├─ 用服务器内容替换
   └─ 标记 InitSync = true (待下载内容)

4. 通知前端
   └─ Web.FixSyncConflictNote(originalNote, copiedNote)

5. 用户后续操作
   ├─ 保留两个版本
   ├─ 合并内容
   └─ 删除冲突副本
```

### 5.4 分块同步机制

```
分块参数:
- notebookMaxEntry = 200
- noteMaxEntry = 200
- tagMaxEntry = 200

分块逻辑:
API 调用: GetSyncNotes(afterUsn, maxEntry)

if (len(notes) == maxEntry) {
    // 可能还有更多数据
    ProcessChunk(notes)
    lastUsn = notes[len-1].Usn
    
    // 500ms 延迟后继续
    time.Sleep(500 * time.Millisecond)
    SyncNote(lastUsn)
} else {
    // 没有更多数据
    isLastChunk = true
    ProcessChunk(notes)
}
```

### 5.5 同步状态管理

```
同步状态字段 (user.js):

LastSyncUsn: 上次成功同步的全局 USN
LastSyncTime: 上次同步时间
NotebookUsn: 笔记本同步进度 (全量同步用)
NoteUsn: 笔记同步进度 (全量同步用)
TagUsn: 标签同步进度 (全量同步用)

更新时机:
- Pull 每个分块后更新对应类型的 USN
- 全部 Pull 完成后更新 LastSyncUsn
- Send Changes 成功后更新 LastSyncUsn
```

### 5.6 Go 同步服务设计

```go
// sync/sync.go
package sync

import (
    "context"
    "sync"
    
    "leanote-desktop/db"
    "leanote-desktop/api"
)

type SyncService struct {
    db              *db.Database
    api             *api.Client
    maxEntry        int
    isSyncing       bool
    needSyncAgain   bool
    retryCount      int
    mu              sync.Mutex
    progressCB      func(progress float64)
}

func NewSyncService(db *db.Database, api *api.Client) *SyncService {
    return &SyncService{
        db:       db,
        api:      api,
        maxEntry: 200,
    }
}

// 全量同步
func (s *SyncService) FullSync() (*SyncInfo, error) {
    s.mu.Lock()
    if s.isSyncing {
        s.mu.Unlock()
        return nil, ErrAlreadySyncing
    }
    s.isSyncing = true
    s.mu.Unlock()
    
    defer func() {
        s.mu.Lock()
        s.isSyncing = false
        s.mu.Unlock()
    }()
    
    syncInfo := &SyncInfo{}
    
    // 1. 获取本地同步状态
    lastUsn, notebookUsn, noteUsn, tagUsn, err := s.db.GetAllLastSyncState()
    if err != nil {
        return nil, err
    }
    
    // 2. 获取服务器状态
    serverState, err := s.api.GetLastSyncState()
    if err != nil {
        return nil, err
    }
    
    // 3. Pull 阶段
    if err := s.SyncNotebooks(notebookUsn, syncInfo); err != nil {
        return nil, err
    }
    s.updateProgress(10)
    
    if err := s.SyncNotes(noteUsn, syncInfo); err != nil {
        return nil, err
    }
    s.updateProgress(40)
    
    if err := s.SyncTags(tagUsn, syncInfo); err != nil {
        return nil, err
    }
    s.updateProgress(50)
    
    // 4. Push 阶段
    if err := s.SendChanges(syncInfo); err != nil {
        return nil, err
    }
    s.updateProgress(100)
    
    // 5. 更新同步状态
    if err := s.db.UpdateLastSyncState(); err != nil {
        return nil, err
    }
    
    return syncInfo, nil
}

// 同步笔记 (含冲突检测)
func (s *SyncService) SyncNotes(afterUsn int64, syncInfo *SyncInfo) error {
    currentUsn := afterUsn
    
    for {
        // 分块获取
        notes, err := s.api.GetSyncNotes(currentUsn, s.maxEntry)
        if err != nil {
            return err
        }
        
        if len(notes) == 0 {
            break
        }
        
        // 处理每一条
        for _, serverNote := range notes {
            if err := s.ProcessNoteSync(serverNote, syncInfo); err != nil {
                logrus.Errorf("process note sync error: %v", err)
            }
        }
        
        // 更新 USN
        if len(notes) > 0 {
            currentUsn = notes[len(notes)-1].Usn
            s.db.UpdateNoteUsn(currentUsn)
        }
        
        // 检查是否还有更多
        if len(notes) < s.maxEntry {
            break
        }
        
        time.Sleep(500 * time.Millisecond)
    }
    
    return nil
}

// 处理单个笔记同步
func (s *SyncService) ProcessNoteSync(serverNote *models.Note, syncInfo *SyncInfo) error {
    // 1. 服务器已删除
    if serverNote.IsDeleted {
        localID, _ := s.db.GetLocalNoteID(serverNote.NoteID)
        if localID != "" {
            s.db.DeleteNoteForce(localID)
            syncInfo.Note.Deletes = append(syncInfo.Note.Deletes, localID)
        }
        return nil
    }
    
    // 2. 查找本地笔记
    localNote, err := s.db.GetNoteByServerID(serverNote.NoteID)
    if err != nil {
        return err
    }
    
    if localNote == nil {
        // 本地没有 → 新建
        created, err := s.db.AddNoteForce(serverNote)
        if err != nil {
            return err
        }
        syncInfo.Note.Adds = append(syncInfo.Note.Adds, created)
        return nil
    }
    
    // USN 相同，未变更
    if localNote.Usn == serverNote.Usn {
        return nil
    }
    
    // 检测冲突
    if localNote.IsDirty {
        // 获取服务器内容
        serverContent, err := s.api.GetNoteContent(serverNote.NoteID)
        if err != nil {
            return err
        }
        
        if serverContent == localNote.Content {
            // 内容相同 → 不冲突
            return s.db.UpdateNoteForce(serverNote, false)
        }
        
        // 真冲突
        syncInfo.Note.Conflicts = append(syncInfo.Note.Conflicts, &SyncConflict{
            Server: serverNote,
            Local:  localNote,
        })
        return nil
    }
    
    // 本地未修改 → 直接更新
    return s.db.UpdateNoteForce(serverNote, true)
}
```

---

## 6. 测试策略

### 6.1 测试金字塔

```
                    ┌──────────────┐
                    │   E2E Test   │
                    │  (Mock API)  │
                    │    5-10个    │
                    └──────────────┘
                           ▲
                    ┌──────┴───────┐
                    │ Integration  │
                    │    Test      │
                    │   30-50个    │
                    └──────────────┘
                           ▲
              ┌────────────┴────────────┐
              │      Unit Test          │
              │    100-200个            │
              └─────────────────────────┘
```

### 6.2 单元测试

#### 6.2.1 覆盖范围

| 模块 | 测试重点 | 用例数 |
|------|----------|--------|
| USN 比较 | 边界条件、同步判断 | 10+ |
| 冲突检测 | 内容比较、IsDirty 状态 | 15+ |
| ID 映射 | 本地ID ↔ 服务器ID | 10+ |
| 分块逻辑 | 边界、空数据 | 10+ |
| 数据转换 | NeDB → SQLite 类型转换 | 20+ |
| 时间处理 | Go 时间格式解析 | 10+ |

#### 6.2.2 示例测试用例

```go
// sync/sync_test.go
package sync

import (
    "testing"
    
    "leanote-desktop/models"
)

// USN 比较测试
func TestNeedsSync(t *testing.T) {
    tests := []struct {
        serverUsn int64
        localUsn  int64
        expected  bool
    }{
        {100, 50, true},   // 服务器更新，需要同步
        {100, 100, false}, // 相同，不需要同步
        {50, 100, false},  // 异常情况
    }
    
    for _, tt := range tests {
        result := NeedsSync(tt.serverUsn, tt.localUsn)
        if result != tt.expected {
            t.Errorf("NeedsSync(%d, %d) = %v, want %v", 
                tt.serverUsn, tt.localUsn, result, tt.expected)
        }
    }
}

// 冲突检测测试
func TestDetectConflict(t *testing.T) {
    // 本地未修改 → 不冲突
    local := &models.Note{IsDirty: false}
    if DetectConflict(local, "any content") {
        t.Error("expected no conflict when local not dirty")
    }
    
    // 本地已修改 + 内容相同 → 不冲突
    local = &models.Note{IsDirty: true, Content: "same"}
    if DetectConflict(local, "same") {
        t.Error("expected no conflict when content same")
    }
    
    // 本地已修改 + 内容不同 → 冲突
    local = &models.Note{IsDirty: true, Content: "local"}
    if !DetectConflict(local, "server") {
        t.Error("expected conflict when content different")
    }
}

// 分块测试
func TestChunkBoundaries(t *testing.T) {
    // 正好整块
    items := make([]int, 200)
    chunks := ChunkItems(items, 200)
    if len(chunks) != 1 {
        t.Errorf("expected 1 chunk, got %d", len(chunks))
    }
    
    // 多一块
    items = make([]int, 201)
    chunks = ChunkItems(items, 200)
    if len(chunks) != 2 {
        t.Errorf("expected 2 chunks, got %d", len(chunks))
    }
    if len(chunks[1]) != 1 {
        t.Errorf("expected chunk size 1, got %d", len(chunks[1]))
    }
    
    // 空数据
    items = []int{}
    chunks = ChunkItems(items, 200)
    if len(chunks) != 0 {
        t.Errorf("expected 0 chunks, got %d", len(chunks))
    }
}
```

### 6.3 集成测试

#### 6.3.1 测试环境

```
使用 SQLite :memory: 数据库
使用 httptest 模拟 HTTP 服务器
使用临时目录
```

#### 6.3.2 关键测试场景

| 场景 | 描述 | 验证点 |
|------|------|--------|
| 首次同步 | 新用户首次登录 | 所有数据正确拉取 |
| 增量同步 | 有本地变更 | Pull + Push 正确执行 |
| 纯 Pull | 服务器有变更 | 本地数据正确更新 |
| 纯 Push | 本地有变更 | 服务器正确更新 |
| 冲突场景 | 双方都有变更 | 冲突正确处理 |
| 网络错误 | 请求失败 | 重试机制、状态恢复 |
| 大数据量 | 1000+ 笔记 | 分块正确、性能可接受 |
| 断点续传 | 中途中断 | 恢复后正确继续 |

#### 6.3.3 示例测试代码

```go
// sync/integration_test.go
package sync

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    
    "leanote-desktop/db"
    "leanote-desktop/api"
)

func TestFullSyncNewUser(t *testing.T) {
    // 准备内存数据库
    database := db.NewInMemory()
    defer database.Close()
    
    // 启动 Mock 服务器
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/user/getSyncState":
            json.NewEncoder(w).Encode(map[string]interface{}{
                "LastSyncUsn": 100,
            })
        case "/api/notebook/getSyncNotebooks":
            json.NewEncoder(w).Encode([]map[string]interface{}{
                {"NotebookId": "nb1", "Usn": 50, "Title": "Notebook 1"},
                {"NotebookId": "nb2", "Usn": 100, "Title": "Notebook 2"},
            })
        }
    }))
    defer server.Close()
    
    // 创建客户端和服务
    client := api.NewClientWithBaseURL("test_token", server.URL)
    syncSvc := NewSyncService(database, client)
    
    // 执行同步
    result, err := syncSvc.FullSync()
    if err != nil {
        t.Fatalf("FullSync failed: %v", err)
    }
    
    // 验证
    if len(result.Notebook.Adds) != 2 {
        t.Errorf("expected 2 notebooks, got %d", len(result.Notebook.Adds))
    }
    
    notebooks, _ := database.GetNotebooks()
    if len(notebooks) != 2 {
        t.Errorf("expected 2 notebooks in db, got %d", len(notebooks))
    }
}

func TestConflictHandling(t *testing.T) {
    database := db.NewInMemory()
    defer database.Close()
    
    // 创建本地已修改笔记
    local := &models.Note{
        NoteID:       "local_id",
        ServerNoteID: "server_id",
        Content:      "local content",
        IsDirty:      true,
    }
    database.InsertNote(local)
    
    // Mock 服务器返回不同内容
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/api/note/getNoteContent" {
            json.NewEncoder(w).Encode(map[string]interface{}{
                "Content": "server content",
            })
        }
    }))
    defer server.Close()
    
    client := api.NewClientWithBaseURL("test_token", server.URL)
    syncSvc := NewSyncService(database, client)
    
    // 处理同步
    serverNote := &models.Note{
        NoteID: "server_id",
        Usn:    200,
    }
    syncInfo := &SyncInfo{}
    
    err := syncSvc.ProcessNoteSync(serverNote, syncInfo)
    if err != nil {
        t.Fatalf("ProcessNoteSync failed: %v", err)
    }
    
    // 验证冲突被检测
    if len(syncInfo.Note.Conflicts) != 1 {
        t.Errorf("expected 1 conflict, got %d", len(syncInfo.Note.Conflicts))
    }
}
```

### 6.4 E2E 测试

#### 6.4.1 测试环境

```
使用 httptest 启动 Mock 服务器
使用临时 SQLite 文件
使用临时数据目录
```

#### 6.4.2 测试场景

```
1. 完整同步流程
   - Mock 完整 API 响应
   - 验证数据库最终状态
   - 验证进度回调

2. 网络异常恢复
   - 模拟网络中断
   - 验证重试机制
   - 验证数据一致性

3. 大数据量同步
   - 生成大量测试数据
   - 验证内存使用
   - 验证同步时间
```

### 6.5 数据一致性验证

#### 6.5.1 迁移后验证

```sql
-- 记录数量对比
SELECT 'notebooks' as type, COUNT(*) as nedb_count FROM nedb_notebooks
UNION ALL
SELECT 'notebooks', COUNT(*) FROM sqlite_notebooks;

-- 关键字段抽样
SELECT note_id, title, usn, is_dirty 
FROM notes 
WHERE note_id IN ('sample_id_1', 'sample_id_2', 'sample_id_3');

-- 外键完整性
SELECT n.note_id 
FROM notes n 
LEFT JOIN notebooks nb ON n.notebook_id = nb.notebook_id 
WHERE nb.notebook_id IS NULL;
```

#### 6.5.2 同步一致性验证

```
验证点:
1. USN 序列正确递增
2. IsDirty 状态正确更新
3. 本地ID与服务器ID映射正确
4. 冲突笔记内容完整保留
5. 附件和图片关联正确
```

### 6.6 测试覆盖率目标

| 模块 | 目标覆盖率 | 说明 |
|------|-----------|------|
| 同步服务 | 90% | 核心业务逻辑 |
| 数据库层 | 95% | CRUD、事务、索引 |
| API 客户端 | 85% | 请求、响应、错误 |
| 冲突处理 | 95% | 所有冲突场景 |
| 数据迁移 | 90% | 类型转换、完整性 |

### 6.7 运行测试

```bash
# 运行所有测试
go test ./...

# 运行带覆盖率的测试
go test -cover ./...

# 生成覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# 运行特定包的测试
go test ./sync/...
go test ./db/...

# 运行基准测试
go test -bench=. ./...
```

---

## 7. 迁移执行计划

### 7.1 阶段划分

```
Phase 0: 准备阶段 (2天)
├─ 创建 Wails 项目骨架
├─ 初始化 Go 模块
├─ 配置依赖
└─ 编写数据模型

Phase 1: 数据库层 (1周)
├─ 创建 SQLite 表结构
├─ 实现 Repository 层
├─ 实现连接管理
├─ 编写数据库单元测试
└─ 实现数据迁移工具

Phase 2: API 客户端 (3天)
├─ 实现 HTTP 客户端封装
├─ 实现认证 API
├─ 实现笔记本 API
├─ 实现笔记 API
├─ 实现文件/图片 API
└─ 编写 API 单元测试

Phase 3: 同步服务 (1.5周)
├─ 实现 USN 管理
├─ 实现 Pull 逻辑
├─ 实现 Push 逻辑
├─ 实现冲突检测
├─ 实现冲突解决
├─ 编写同步单元测试
└─ 编写同步集成测试

Phase 4: Wails 绑定 (3天)
├─ 实现用户相关绑定
├─ 实现笔记本相关绑定
├─ 实现笔记相关绑定
├─ 实现同步相关绑定
└─ 实现文件相关绑定

Phase 5: 前端适配 (3天)
├─ 替换 ipcRenderer 为 Wails 调用
├─ 替换 @electron/remote
├─ 替换 fs 操作
├─ 适配自定义协议
└─ 测试前端功能

Phase 6: 集成测试 (3天)
├─ 完整同步流程测试
├─ 数据迁移测试
├─ 性能测试
└─ Bug 修复

Phase 7: 打包发布 (2天)
├─ 配置打包脚本
├─ 多平台编译测试
├─ 编写更新日志
└─ 发布测试版
```

### 7.2 详细任务清单

#### Phase 0: 准备阶段

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| 安装 Wails CLI | 0.5天 | wails 命令可用 |
| 创建项目 | 0.5天 | 项目骨架 |
| 初始化 Go 模块 | 0.5天 | go.mod |
| 设计数据模型 | 0.5天 | models/*.go |

#### Phase 1: 数据库层

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| 创建 SQLite 表结构 | 0.5天 | db/migrations.sql |
| 实现数据库连接 | 0.5天 | db/db.go |
| 实现 User Repository | 0.5天 | db/user_repo.go |
| 实现 Notebook Repository | 0.5天 | db/notebook_repo.go |
| 实现 Note Repository | 1天 | db/note_repo.go |
| 实现 Tag Repository | 0.5天 | db/tag_repo.go |
| 实现其他 Repository | 0.5天 | db/attach_repo.go 等 |
| 实现数据迁移工具 | 1天 | migration/*.go |
| 编写单元测试 | 1天 | db/*_test.go |

#### Phase 2: API 客户端

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| HTTP 客户端封装 | 0.5天 | api/client.go |
| 认证 API | 0.5天 | api/auth.go |
| 笔记本 API | 0.5天 | api/notebook.go |
| 笔记 API | 1天 | api/note.go |
| 文件 API | 0.5天 | api/file.go |
| 单元测试 | 1天 | api/*_test.go |

#### Phase 3: 同步服务

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| 同步服务框架 | 0.5天 | sync/sync.go |
| USN 管理 | 0.5天 | sync/sync.go |
| Pull 逻辑 | 1.5天 | sync/pull.go |
| Push 逻辑 | 1.5天 | sync/push.go |
| 冲突检测 | 1天 | sync/conflict.go |
| 冲突解决 | 1天 | sync/conflict.go |
| 进度管理 | 0.5天 | sync/progress.go |
| 单元测试 | 1天 | sync/*_test.go |
| 集成测试 | 1天 | tests/integration/*.go |

#### Phase 4: Wails 绑定

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| 用户绑定 | 0.5天 | app.go (用户方法) |
| 笔记本绑定 | 0.5天 | app.go (笔记本方法) |
| 笔记绑定 | 1天 | app.go (笔记方法) |
| 同步绑定 | 0.5天 | app.go (同步方法) |
| 文件绑定 | 0.5天 | app.go (文件方法) |

#### Phase 5: 前端适配

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| IPC 替换 | 1天 | frontend/src/*.js |
| @electron/remote 替换 | 0.5天 | frontend/src/*.js |
| fs 操作替换 | 0.5天 | 文件操作相关 JS |
| 协议适配 | 0.5天 | AssetServer 配置 |
| 功能测试 | 0.5天 | - |

#### Phase 6: 集成测试

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| 完整同步流程测试 | 1天 | 测试报告 |
| 数据迁移测试 | 1天 | 测试报告 |
| 性能测试 | 0.5天 | 性能报告 |
| Bug 修复 | 0.5天 | - |

#### Phase 7: 打包发布

| 任务 | 预计时间 | 输出 |
|------|----------|------|
| 打包配置 | 0.5天 | wails.json |
| 多平台编译 | 1天 | Windows/macOS/Linux 包 |
| 文档编写 | 0.5天 | README, CHANGELOG |

### 7.3 里程碑

| 里程碑 | 时间 | 交付物 |
|--------|------|--------|
| M1: 数据库层完成 | 第1周末 | 可运行的数据库层 + 测试 |
| M2: API 层完成 | 第2周中 | 可运行的 API 客户端 |
| M3: 同步服务完成 | 第3周末 | 完整同步功能 + 测试 |
| M4: 功能完整 | 第4周中 | 所有功能可用 |
| M5: 测试完成 | 第5周末 | 通过所有测试 |
| M6: 发布就绪 | 第6周末 | 可发布版本 |

---

## 8. 风险管理

### 8.1 技术风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 同步算法复杂度高 | 高 | 高 | 先完整理解现有逻辑，编写详细设计文档 |
| NeDB 数据格式解析问题 | 中 | 中 | 编写解析器单元测试，覆盖边界情况 |
| 冲突处理遗漏场景 | 中 | 高 | 基于现有代码梳理所有冲突场景 |
| 前端兼容性问题 | 低 | 中 | 保持前端代码最小改动 |
| Wails API 差异 | 低 | 低 | 查阅官方文档，社区支持 |

### 8.2 数据风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 数据迁移丢失 | 低 | 高 | 完整备份，迁移后验证 |
| 数据类型转换错误 | 中 | 中 | 单元测试覆盖所有类型 |
| 外键约束失败 | 中 | 中 | 迁移顺序正确，外键检查 |

### 8.3 进度风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 同步服务超期 | 中 | 高 | 预留缓冲时间，优先核心功能 |
| 测试不充分 | 中 | 中 | 测试驱动开发，持续集成 |
| Bug 修复超期 | 低 | 中 | 优先级排序，关键 Bug 优先 |

### 8.4 回滚计划

```
如果迁移失败，可以回滚到 Electron 版本:

1. 保留 NeDB 数据文件不删除
2. 新版本使用独立的 SQLite 数据文件
3. 卸载新版本，安装旧版本即可恢复
4. 数据迁移工具支持双向转换（可选）
```

---

## 附录

### A. 参考资料

- [Wails 官方文档](https://wails.io/docs/introduction)
- [modernc.org/sqlite 文档](https://pkg.go.dev/modernc.org/sqlite)
- [go-resty 文档](https://github.com/go-resty/resty)
- [Go 测试指南](https://go.dev/doc/tutorial/add-a-test)
- [Leanote API 文档](https://github.com/leanote/leanote/wiki)

### B. 关键代码位置参考

| 功能 | 原文件 | 关键行号 |
|------|--------|----------|
| 全量同步 | sync.js | 585-660 |
| 增量同步 | sync.js | 762-850 |
| Pull 笔记本 | sync.js | 138-261 |
| Pull 笔记 | sync.js | 280-441 |
| 冲突检测 | sync.js | 347-391 |
| Push 笔记 | sync.js | 966-1122 |
| 笔记冲突处理 | note.js | 1267-1428 |
| 数据库初始化 | db.js | 20-85 |
| IPC 数据库操作 | db_main.js | 43-94 |

### C. 常用命令

```bash
# 开发模式运行
wails dev

# 构建生产版本
wails build

# 构建特定平台
wails build -platform windows/amd64
wails build -platform darwin/amd64
wails build -platform darwin/arm64
wails build -platform linux/amd64

# 生成绑定
wails generate module

# 运行测试
go test ./...

# 运行测试（带覆盖率）
go test -cover ./...

# 代码检查
go vet ./...

# 格式化代码
go fmt ./...
```

### D. 测试检查清单

#### 数据库层测试

- [ ] 用户 CRUD 操作
- [ ] 笔记本 CRUD 操作
- [ ] 笔记 CRUD 操作
- [ ] 标签 CRUD 操作
- [ ] 外键约束
- [ ] 索引查询性能
- [ ] 全文搜索

#### 同步服务测试

- [ ] 全量同步 - 新用户
- [ ] 全量同步 - 有本地数据
- [ ] 增量同步 - 仅 Pull
- [ ] 增量同步 - 仅 Push
- [ ] 增量同步 - Pull + Push
- [ ] 冲突检测 - 内容相同
- [ ] 冲突检测 - 内容不同
- [ ] 冲突解决 - 复制笔记
- [ ] 分块同步 - 边界条件
- [ ] 网络错误 - 重试机制
- [ ] 大数据量 - 性能

#### 数据迁移测试

- [ ] 用户数据迁移
- [ ] 笔记本数据迁移
- [ ] 笔记数据迁移
- [ ] 标签数据迁移
- [ ] 附件数据迁移
- [ ] 图片数据迁移
- [ ] 数据完整性验证
- [ ] 外键完整性

#### E2E 测试

- [ ] 完整同步流程
- [ ] 网络中断恢复
- [ ] 并发同步
- [ ] 大数据量同步

---

> 文档维护: 随着迁移进度更新本文档  
> 最后更新: 2026-04-17
