# Desktop Release 构建

Gemsnote Desktop 使用 Wails v2.12.0。GUI 构建依赖宿主系统的原生工具链，因此 Linux、macOS 和 Windows 发布包必须分别在对应操作系统及 CPU 架构上构建，不支持通过 `GOOS`/`GOARCH` 交叉编译。

Desktop 仓库还会复用 Gemsnote 服务端仓库中的 Vue 前端和 TinyMCE 资源。目录必须保持为：

```text
gemsnote/
├── frontend/
├── public/tinymce/
└── desktop-app/
```

版本号集中定义在 `desktop-app/api2/version.go` 的 `ClientVersion`。脚本会拒绝与该版本不一致的构建参数。

## 支持的平台

| 平台 | 构建宿主 | 产物 |
| --- | --- | --- |
| Linux amd64 | Linux amd64 | `gemsnote-<version>-linux-amd64.zip`、`gemsnote-<version>-linux-amd64.AppImage` |
| Linux arm64 | Linux arm64 | `gemsnote-<version>-linux-arm64.zip`、`gemsnote-<version>-linux-arm64.AppImage` |
| macOS amd64 | Intel macOS | `gemsnote-<version>-darwin-amd64.dmg` |
| macOS arm64 | Apple Silicon macOS | `gemsnote-<version>-darwin-arm64.dmg` |
| Windows amd64 | Windows amd64 | `gemsnote-<version>-windows-amd64-installer.exe`（NSIS） |

每次构建还会更新输出目录中的 `SHA256SUMS`，其中只包含当前版本的全部平台产物，不会混入同一目录中的旧版本。

## 前置依赖

所有平台需要 Go 1.23 或更高版本、Node.js 22、npm，以及 Wails CLI v2.12.0：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
```

如果安装后仍提示 `Required tool not found: wails`，需要将 Go 的可执行文件目录加入 `PATH`：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Windows PowerShell：

```powershell
$env:Path = "$(go env GOPATH)\bin;$env:Path"
```

将该目录加入系统或用户的永久 `PATH` 后，新打开的终端也可以直接运行 `wails version`。

Linux 还需要 `zip`、`sha256sum` 和 `appimagetool`：

```bash
sudo apt-get install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
```

`appimagetool` 必须下载与 Linux CPU 架构匹配的版本：amd64 下载 `appimagetool-x86_64.AppImage`，arm64 下载 `appimagetool-aarch64.AppImage`。从 [GitHub Releases](https://github.com/AppImage/appimagetool/releases) 下载后，必须重命名为 `appimagetool`、增加可执行权限并放入 `PATH`，例如：

```bash
mv appimagetool-x86_64.AppImage appimagetool
chmod +x appimagetool
sudo mv appimagetool /usr/local/bin/
```

如果 `appimagetool` 无法自动下载 Type 2 runtime，可从 [type2-runtime Releases](https://github.com/AppImage/type2-runtime/releases) 手工下载与构建平台匹配的文件，然后通过 `APPIMAGE_RUNTIME_FILE` 传给构建脚本。amd64 使用 `runtime-x86_64`，arm64 使用 `runtime-aarch64`：

```bash
APPIMAGE_RUNTIME_FILE=/absolute/path/to/runtime-x86_64 \
  scripts/build-release.sh 1.0.0 linux amd64 /absolute/path/to/release
```

路径必须指向实际 runtime 文件，建议使用绝对路径。脚本会将其作为 `appimagetool --runtime-file` 参数传入，因此构建期间不再需要在线下载 runtime。

Linux ZIP 内含 `gemsnote.desktop` 和图标，手动安装时可将程序目录加入 `PATH`，再把 `.desktop` 文件复制到 `~/.local/share/applications/`。

Windows 脚本依赖 PowerShell 和 Git for Windows 提供的 `bash`，因为 Wails 的前端构建钩子会调用 `build-frontend.sh`。

## Linux 和 macOS

在 `desktop-app` 目录运行：

```bash
scripts/build-release.sh 1.0.0
```

完整参数形式为：

```text
scripts/build-release.sh <version> [linux|darwin] [amd64|arm64] [absolute-output-dir]
```

平台、架构和输出目录可以省略，默认使用当前宿主平台、宿主架构和 `desktop-app/release/`。指定的平台和架构必须与宿主一致，输出目录如果指定则必须是绝对路径。

## Windows

在 PowerShell 中运行：

```powershell
.\scripts\build-release.ps1 -Version 1.0.0
```

也可以指定绝对输出目录：

```powershell
.\scripts\build-release.ps1 -Version 1.0.0 -Platform windows -Arch amd64 -OutputDir C:\release
```

## 构建流程

两个脚本执行相同的检查和构建步骤：

1. 校验版本格式以及 `api.ClientVersion`；
2. 校验 Wails v2.12.0、Go、Node/npm 和共享前端目录；
3. 执行前端 `npm ci`、测试和生产构建；
4. 执行 Desktop 全量 Go 测试；
5. 使用 Wails 在宿主平台原生构建；
6. 打包产物并更新 SHA-256 校验文件。

Linux 会同时生成带 `.desktop` 和图标的 ZIP 以及 AppImage；macOS 会将 `.app` 制作为 DMG；Windows 会使用 Wails 的 NSIS 模板生成安装器 EXE。Windows 不生成 MSI。

macOS 发布给其他用户前还应完成应用签名和 Apple notarization；Windows 正式分发可进一步增加 Authenticode 签名或 NSIS 安装包。这些签名材料不应写入仓库。

GitHub tag 发布使用 `.github/workflows/release.yml` 自动执行同样的五平台构建。正式 tag 必须为 `vMAJOR.MINOR.PATCH`，且主仓库和 Desktop 仓库应存在同名 tag，以固定共享前端版本。
