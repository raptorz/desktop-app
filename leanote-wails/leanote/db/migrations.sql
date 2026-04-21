-- Leanote SQLite 数据库初始化脚本

PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS users (
    _id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    email TEXT,
    pwd TEXT,
    token TEXT,
    host TEXT,
    last_sync_usn INTEGER DEFAULT -1,
    last_sync_time INTEGER,
    notebook_usn INTEGER DEFAULT -1,
    note_usn INTEGER DEFAULT -1,
    tag_usn INTEGER DEFAULT -1,
    is_active INTEGER DEFAULT 0,
    is_local INTEGER DEFAULT 0,
    has_db INTEGER DEFAULT 0,
    state TEXT,
    created_time INTEGER,
    last_login_time INTEGER
);

CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active);

CREATE TABLE IF NOT EXISTS notebooks (
    _id TEXT PRIMARY KEY,
    notebook_id TEXT NOT NULL UNIQUE,
    server_notebook_id TEXT,
    user_id TEXT NOT NULL,
    parent_notebook_id TEXT,
    title TEXT NOT NULL,
    seq INTEGER DEFAULT 0,
    number_notes INTEGER DEFAULT 0,
    url_title TEXT,
    is_blog INTEGER DEFAULT 0,
    is_trash INTEGER DEFAULT 0,
    usn INTEGER DEFAULT 0,
    is_dirty INTEGER DEFAULT 0,
    local_is_new INTEGER DEFAULT 0,
    local_is_delete INTEGER DEFAULT 0,
    created_time INTEGER,
    updated_time INTEGER,
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX IF NOT EXISTS idx_notebooks_user ON notebooks(user_id);
CREATE INDEX IF NOT EXISTS idx_notebooks_server_id ON notebooks(server_notebook_id);
CREATE INDEX IF NOT EXISTS idx_notebooks_dirty ON notebooks(user_id, is_dirty);
CREATE INDEX IF NOT EXISTS idx_notebooks_parent ON notebooks(parent_notebook_id);

CREATE TABLE IF NOT EXISTS notes (
    _id TEXT PRIMARY KEY,
    note_id TEXT NOT NULL UNIQUE,
    server_note_id TEXT,
    notebook_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT,
    content TEXT,
    desc TEXT,
    abstract TEXT,
    img_src TEXT,
    tags TEXT,
    is_markdown INTEGER DEFAULT 0,
    is_trash INTEGER DEFAULT 0,
    is_blog INTEGER DEFAULT 0,
    is_star INTEGER DEFAULT 0,
    usn INTEGER DEFAULT 0,
    is_dirty INTEGER DEFAULT 0,
    content_is_dirty INTEGER DEFAULT 0,
    local_is_new INTEGER DEFAULT 0,
    local_is_delete INTEGER DEFAULT 0,
    init_sync INTEGER DEFAULT 0,
    conflict_note_id TEXT,
    conflict_time INTEGER,
    conflict_fixed INTEGER DEFAULT 0,
    err TEXT,
    created_time INTEGER,
    updated_time INTEGER,
    FOREIGN KEY (notebook_id) REFERENCES notebooks(notebook_id),
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX IF NOT EXISTS idx_notes_user ON notes(user_id);
CREATE INDEX IF NOT EXISTS idx_notes_notebook ON notes(notebook_id);
CREATE INDEX IF NOT EXISTS idx_notes_server_id ON notes(server_note_id);
CREATE INDEX IF NOT EXISTS idx_notes_dirty ON notes(user_id, is_dirty);
CREATE INDEX IF NOT EXISTS idx_notes_trash ON notes(user_id, is_trash);
CREATE INDEX IF NOT EXISTS idx_notes_star ON notes(user_id, is_star);

CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
    title,
    content,
    content='notes',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS notes_ai AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts(rowid, title, content) 
    VALUES (new.rowid, new.title, new.content);
END;

CREATE TRIGGER IF NOT EXISTS notes_ad AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, content) 
    VALUES('delete', old.rowid, old.title, old.content);
END;

CREATE TRIGGER IF NOT EXISTS notes_au AFTER UPDATE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, content) 
    VALUES('delete', old.rowid, old.title, old.content);
    INSERT INTO notes_fts(rowid, title, content) 
    VALUES (new.rowid, new.title, new.content);
END;

CREATE TABLE IF NOT EXISTS tags (
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

CREATE INDEX IF NOT EXISTS idx_tags_user ON tags(user_id);
CREATE INDEX IF NOT EXISTS idx_tags_dirty ON tags(user_id, is_dirty);

CREATE TABLE IF NOT EXISTS note_histories (
    _id INTEGER PRIMARY KEY AUTOINCREMENT,
    note_id TEXT NOT NULL,
    content TEXT,
    updated_time INTEGER,
    FOREIGN KEY (note_id) REFERENCES notes(note_id)
);

CREATE INDEX IF NOT EXISTS idx_histories_note ON note_histories(note_id);

CREATE TABLE IF NOT EXISTS attachs (
    _id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    server_file_id TEXT,
    note_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT,
    type TEXT,
    path TEXT,
    is_attach INTEGER DEFAULT 1,
    is_dirty INTEGER DEFAULT 0,
    created_time INTEGER,
    FOREIGN KEY (note_id) REFERENCES notes(note_id),
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX IF NOT EXISTS idx_attachs_note ON attachs(note_id);
CREATE INDEX IF NOT EXISTS idx_attachs_server_id ON attachs(server_file_id);

CREATE TABLE IF NOT EXISTS images (
    _id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL UNIQUE,
    server_file_id TEXT,
    user_id TEXT NOT NULL,
    path TEXT,
    is_dirty INTEGER DEFAULT 0,
    created_time INTEGER,
    FOREIGN KEY (user_id) REFERENCES users(_id)
);

CREATE INDEX IF NOT EXISTS idx_images_server_id ON images(server_file_id);

CREATE TABLE IF NOT EXISTS config (
    key TEXT PRIMARY KEY,
    value TEXT
);
