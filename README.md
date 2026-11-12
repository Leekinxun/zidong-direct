<div align="center">

<img src="./web/public/favicon.svg" width="92" height="92" alt="紫东直连 Zidong Direct Logo" />

# 紫东直连 · Zidong Direct

**轻量、纯网页版的多源数据库直连工作台（Go 单二进制 + React + Docker）**

*A lightweight, Docker-ready Web Database Studio inspired by Navicat & macOS*

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)](https://react.dev/)
[![Docker](https://img.shields.io/badge/Docker-Single%20Image-2496ED?logo=docker&logoColor=white)](./Dockerfile)
[![Win7 Compatible](https://img.shields.io/badge/Win7%20Browser-Chrome%20109%20%7C%20Firefox%20115%2B-4CAF50)](#windows-7-客户端兼容性说明)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

[简体中文](#简体中文) · [English](#english)

</div>

---

## 简体中文

**紫东直连 (Zidong Direct)** 是一款面向运维、研发与企业内网场景的纯 Web 数据库管理工作台。后端采用 **Go 纯原生协议驱动**（无 CGO / 零 Java JRE / 零 Oracle Instant Client 依赖），打包为单个轻量 Docker 容器即可提供 **MySQL、PostgreSQL、SQLite、SQL Server、Oracle、达梦 (DM7/DM8)** 六大数据库的一站式直连、SQL 查询、在线改数、表结构设计与导入导出能力。

前端在交互逻辑上对齐经典桌面数据库工具（Navicat 式左树 + 中央多标签工作区 + 右侧属性与 DDL 面板），视觉采用 **Apple macOS 暗色毛玻璃风格**，并在构建链路中严格校验 **Windows 7 最高兼容浏览器（Chrome 109 / Firefox 115 ESR）** 兼容性。

### ✨ 核心亮点

- **零重型依赖，单容器秒级启动**：纯 Go 编译，无需 Java 环境（对比动辄数百 MB 内存的 Java Web 工具更省资源），无需在宿主机安装 Oracle / 达梦 C 客户端。
- **支持国际主流与国产信创数据库**：原生支持 MySQL/MariaDB、PostgreSQL、SQLite 3、SQL Server、Oracle 与 **达梦 DM7 / DM8**。
- **开箱即用的 Windows 7 老机器兼容**：前端编译目标锁定 `chrome109` 与 `firefox115`，内网老旧 Win7 终端无需安装任何桌面客户端软件，打开浏览器即可使用。
- **Navicat 式工作区与 macOS 视觉体验**：
  - **可拉伸左侧导航树**：按住连接树右边缘可自由拖拽调整宽度（`220px`~`680px`，自动记忆，双击恢复默认）；点击展开连接/库/模式/表，**启动时不自动全量拉取表列表**，面对上百张大表依然秒开。
  - **中央多标签工作台**：同屏多标签页切换数据网格、SQL 查询编辑器与表结构设计器；右侧栏随时预览对象常规属性、字段列表与自动生成的 `CREATE TABLE` DDL。

### 🗄️ 支持的数据库引擎

| 数据库引擎 | 纯 Go 协议驱动 | 默认端口 | 多 Schema 浏览 | SSH 隧道 | 在线改表 DDL | 说明 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| **MySQL / MariaDB** | `go-sql-driver/mysql` | `3306` | - | ✅ | ✅ | 支持 UTF8MB4、多库浏览与在线表设计 |
| **PostgreSQL** | `jackc/pgx/v5` | `5432` | ✅ | ✅ | ✅ | 兼容 PG 9.5 ~ PG 18（支持 `SCRAM-SHA-256`、`prefer` SSL 自适应回退、PG 12+ `STORED` 与 PG 18 `VIRTUAL` 生成列） |
| **SQLite 3** | `modernc.org/sqlite` | 本地文件 | - | - | ⚠️ 表重建 | 支持容器挂载浏览、浏览器上传/下载及新建 `.db`；改列自动生成事务重建表 DDL |
| **SQL Server** | `microsoft/go-mssqldb` | `1433` | ✅ | ✅ | ✅ | 支持 Schema 浏览、TOP 分页与可选 `trustServerCert` |
| **Oracle** | `sijms/go-ora/v2` | `1521` | ✅ | ✅ | ✅ | 纯 Go 协议直连（无需 Instant Client），支持 **Service Name** 与 **SID** |
| **达梦数据库 DM7 / DM8** | `gitee.com/chunanyong/dm` | `5236` | ✅ | ✅ | ✅ | 纯 Go 协议直连，支持模式/表/视图浏览、在线 CRUD、建表与 `ALTER` |

### 🛠️ 功能清单

- **对象浏览与快捷维护**
  - 按需展开 `连接 -> 数据库 -> Schema -> 表 / 视图`。
  - 顶部功能区支持 `连接 / 新建查询 / 表总览 / 视图 / 查询历史 / 新建表 / 导入 / 导出`。
  - 支持列表详情视图与图标网格视图切换，支持右键删除表、清空表 (`DELETE FROM`)、截断表 (`TRUNCATE`) 等表维护操作。
- **表数据网格（在线增删改查）**
  - 支持点击列头升/降序、多条件参数化筛选（防 SQL 注入）、页码与每页条数切换。
  - 双击单元格直接编辑，支持一键置为 `(NULL)`、新增行、标记删除行，通过底部 `+ / - / ✓ / ✕` 按钮在同一数据库事务内批量提交或撤销。
- **SQL 查询工作台**
  - 基于 CodeMirror 6：按引擎方言高亮，自动加载表名/列名智能补全。
  - 支持 `Ctrl+Enter` / `Cmd+Enter` 执行全篇或仅运行选中片段、分号拆批多语句执行、多结果集标签页展示与 SQL 执行历史本地记忆。
- **可视化表结构设计器**
  - 可视化配置字段名、数据类型建议、允许空、主键 (`PK`)、自增、默认值、PostgreSQL 生成列 (`GENERATED ALWAYS AS (...) STORED / VIRTUAL`)、注释、索引与外键。
  - 支持「修改前先预览差异 DDL SQL」，按实际变更项精准生成 `ALTER COLUMN` 语句，确认无误后再提交执行。
  - 可拖动字段列表的列标题边缘调整宽度，并拖动长文本框右下角调整高度。
  - 从数据库、表或视图的「查看注释」打开属性面板。数据库级注释支持 PostgreSQL、MariaDB 10.5+ 和配置了 `MS_Description` 的 SQL Server。
- **SSH 隧道代理**
  - 支持 SSH 密码认证与私钥认证（含私钥口令 Passphrase），支持 SHA256 主机指纹校验，断开连接或删除连接时自动清理隧道资源。
  - 桌面版可从左侧 SSH 区打开已保存的 SSH 设置，或新建终端页签后直接填写服务器地址和认证信息。终端与数据库页签共用工作区，切换页签时会话继续运行，关闭终端页签会断开连接。
  - 在终端连接表单或数据库窗口的 SSH 页签点「保存 SSH 主机」，可独立保存服务器配置。左侧「已保存的 SSH 主机」中的条目点击即打开终端页签并连接。数据库条目右键可「另存为 SSH 主机」。
  - 从数据库连接窗口的 SSH 页签可测试终端连接，也可点「打开 SSH 终端」把当前输入带入新页签。选中终端内容后用 `Ctrl+Shift+C` 复制，用 `Ctrl+Shift+V` 粘贴。多行粘贴需要确认，`Ctrl+C` 向远端发送中断。
- **数据导入与导出**
  - **导出**支持表数据（含当前筛选与排序）和单条 SQL 查询结果，格式包括 CSV、TXT、TSV、JSON、JSON Lines、XML、HTML、SQL INSERT、Excel（XLS、XLSX）、ODS、DBF 和平面 Parquet。CSV、TXT、TSV 可带 UTF-8 BOM，TXT 支持选择分隔符或按列指定定宽。
  - **导入**支持 CSV、TXT、TSV、定宽文本、JSON、JSON Lines、XML、Excel（XLS、XLSX、XLSM）、ODS、DBF、平面 Parquet 和本地 SQL 脚本。导入向导可预览字段、选择工作表，并按列名、位置或手动建立映射。
  - 桌面版及启用登录的 Web 版可从公开 HTTP(S) 文件 URL 导入表数据，下载时限制文件大小并校验预览与导入的内容一致。Windows 桌面版也可从 ODBC DSN 或 Access（MDB、ACCDB）导入，需要安装与应用位数匹配的驱动。
  - 导入向导每次处理一个来源和一张现有表，可追加、按主键更新、更新或追加、跳过匹配项、删除匹配项，或清空后导入。删除与清空模式会要求确认。
- **安全与权限控制**
  - 数据库密码、SSH 密码及 SSH 私钥使用 **AES-256-GCM** 加密后保存在持久化元数据目录；所有查询接口绝不回传明文秘钥。
  - 支持基于签名 Cookie 的工作台登录保护（通过环境变量一键开关）。

### 🚀 Docker 快速部署

#### 1. 启动服务

```bash
git clone https://github.com/Leekinxun/zidong-direct.git
cd zidong-direct
docker compose up -d --build
```

启动完成后访问：`http://<服务器IP>:8080`

默认工作台登录账号（**部署到局域网或公网前请务必修改 [docker-compose.yml](./docker-compose.yml) 中的密码**）：
- 用户名：`admin`
- 密码：`changeme123`

#### 2. 环境变量说明

| 变量名 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `PORT` | `8080` | HTTP 服务监听端口 |
| `DATA_DIR` | `/data` | 元数据与持久化数据根目录（`docker-compose.yml` 默认映射 `./data:/data`） |
| `SQLITE_DIR` | `/data/sqlite` | SQLite 数据库文件托管目录，可在网页端上传/下载或新建 `.db` |
| `ALLOW_ARBITRARY_SQLITE_PATHS` | `false` | 是否允许填写 `SQLITE_DIR` 目录之外的宿主机/容器绝对路径（默认禁止，提升安全性） |
| `APP_ADMIN_USER` | `admin` | 工作台登录用户名 |
| `APP_ADMIN_PASSWORD` | 空（Compose 中示例为 `changeme123`） | 工作台登录密码；当非空且 `APP_AUTH_DISABLED!=true` 时启用登录拦截 |
| `APP_AUTH_DISABLED` | `false` | 设为 `true` 时关闭工作台登录验证（仅建议本机测试环境使用） |
| `APP_SECRET_KEY` | 空 | 主加密密钥（用于连接凭据与 Cookie 签名）；留空时首次启动自动生成并持久化到 `/data/meta/.master.key` |
| `MAX_QUERY_ROWS` | `1000` | 单次 SQL 查询允许返回的最大行数上限 |
| `MAX_UPLOAD_BYTES` | `268435456` (256 MB) | SQLite / 导入文件上传大小上限（字节） |
| `QUERY_TIMEOUT_SECONDS` | `60` | SQL 查询默认超时时间（秒） |

### 💻 本地源码开发与运行

**环境要求**：Go `1.25+`、Node.js `20+` / `22+`

```bash
# 1. 构建前端（自动执行 TypeScript 类型检查、Vite 打包及 Win7 兼容性检查）
cd web
npm ci
npm run build

# 2. 启动 Go 后端（会自动加载 ../web/dist 静态资源）
cd ../backend
DATA_DIR=../data APP_AUTH_DISABLED=true go run ./cmd/server
```

前端热更新开发模式：

```bash
cd web
npm run dev
# 默认监听 http://localhost:5173，并将 /api 请求代理至 http://127.0.0.1:8080
```

### 🪟 Windows 7 客户端兼容性说明

为了让运行在 **Windows 7 SP1** 上的用户无需升级系统也能流畅使用：

- 推荐使用 **Chrome 109**（Win7 最后一个官方 Chrome 版本）或 **Firefox 115 ESR**（及基于 Chromium 109+ 内核的浏览器）。
- Vite 构建配置（[web/vite.config.ts](./web/vite.config.ts)）已显式指定 `target: ['chrome109', 'firefox115']` 与 `cssTarget: ['chrome109', 'firefox115']`，并采用 Tailwind CSS v3（规避 v4 仅支持新内核的 CSS 函数）。
- 每次执行 `npm run build` 都会运行 [web/scripts/check-win7-compat.mjs](./web/scripts/check-win7-compat.mjs)，自动扫描产物中是否存在仅限 Chrome 110+ / 111+ / 117+ 才支持的 JS/CSS 特性（如 `.toSorted()`、`Object.groupBy`、`color-mix()`、`oklch()` 等）。

### Windows 7 SP1 x64 桌面版

桌面版由 Electron 22.3.27 自带的 Chromium 108 渲染，安装包内置 Go 后端。应用启动时分配回环端口并生成本次启动的访问凭证。数据库连接和 SSH 隧道从终端发起，SQLite 文件可离线使用。目标机无需另装 Node.js、Go 或 Docker。

在现代 Windows 构建机上准备 Node.js 22 和 **Go 1.20.14**，从仓库根目录执行以下命令。

```powershell
npm ci --prefix web
npm ci --prefix desktop
npm test --prefix desktop
npm run package:win7:x64 --prefix desktop
```

安装程序位于 `desktop/dist/Zidong-Direct-Win7-<版本>-x64-Setup.exe`。附加 `-- --dir` 可生成用于调试的未安装目录。构建机已有其他 Go 版本时，可将 `ZIDONG_WIN7_GO` 指向 Go 1.20.14 的 `go.exe`。 [Windows 构建工作流](./.github/workflows/desktop-win7.yml)

桌面连接和 SQLite 文件位于 `%APPDATA%\Zidong Direct\workspace\data`，偏好和查询历史位于同一 `workspace` 目录。卸载保留用户档案。新连接的主密钥由 Windows DPAPI 保护。升级前退出应用并备份 `workspace`，DPAPI 密钥恢复需使用原 Windows 用户档案。

卸载时可从 Windows 开始菜单运行「卸载 紫东直连」，或在应用内选择「文件 → 打开卸载面板」后，在「程序和功能」选择紫东直连。

迁移 Web/Docker 数据前，关闭源服务和桌面版，确认 SQLite 已完成 WAL 检查点，并保留源目录备份。将 `meta/connections.json`、`meta/.master.key` 和 `sqlite/` 复制到一个全新的桌面 `data` 目录。部署通过 `APP_SECRET_KEY` 提供主密钥时，将该环境变量原始字符串的 SHA-256 十六进制摘要写入复制档案的 `meta/.master.key`。桌面后端确认凭据可解密后才将复制的主密钥转换为 DPAPI。以普通用户登录目标机器后先验证连接与 SQLite 再切换使用。

Win7 SP1 x64 安装、签名信任和目标数据库版本须在目标镜像验收。当前 Go 兼容构建的 32 位目标受达梦驱动整数溢出阻塞。Electron 22、Go 1.20 和 Win7 均已结束维护，兼容安装包适用于经过安全评估的受控内网试点。 [Electron 支持说明](https://www.electronjs.org/blog/windows-7-to-8-1-deprecation-notice) · [Go Windows 兼容说明](https://go.dev/doc/go1.20#windows)

#### 在 macOS 开发和测试

准备 Node.js 22 和 Go 1.20.14 或更新版本，首次在仓库根目录安装依赖，再启动桌面应用。

```bash
npm ci --prefix web
npm ci --prefix desktop
npm run dev:electron
```

`npm run dev:electron` 构建 Chromium 108 前端，用 `backend/go.win7.mod` 编译本机 Go 后端，并打开 Electron 22 窗口。代码改动后退出应用并重新运行命令。若 Go 不在 PATH 中，可用 `ZIDONG_DEV_GO=/absolute/path/to/go npm run dev:electron` 指定。

开发数据保存在 `~/Library/Application Support/Zidong Direct Dev/workspace`。用临时档案测试时可执行 `npm run dev:electron -- --user-data-dir="$(mktemp -d)"`。macOS 可用于验证桌面流程与本地 SQLite，Win7 安装和 Windows 凭据保护仍需在目标系统验证。

### 🗂️ 项目结构

```text
zidong-direct/
├── Dockerfile                  # 多阶段构建（Node 构建前端 + 纯 Go 编译后端 + Alpine 运行时）
├── docker-compose.yml          # 一键编排与持久化配置
├── desktop/                    # Electron 主进程、预加载桥接与 Win7 x64 打包
├── backend/
│   ├── cmd/server/main.go      # HTTP 服务入口与静态资源托管
│   └── internal/
│       ├── api/                # REST API 路由、鉴权、元数据/数据/DDL/导入导出处理器
│       ├── config/             # 环境变量加载与默认约束
│       ├── crypto/             # AES-256-GCM 加密器与主密钥管理
│       ├── db/                 # 六引擎 Dialect 适配、连接池、DDL Diff 生成器与 IO 服务
│       ├── sqlutil/            # 多语句 SQL 切分器
│       ├── ssh/                # SSH 隧道转发与主机指纹校验
│       └── store/              # 加密连接配置持久化与 SQLite 路径安全解析
└── web/
    ├── public/favicon.svg      # 紫东直连矢量 Logo
    ├── scripts/                # Win7 浏览器语法静态门禁脚本
    └── src/
        ├── api/client.ts       # 前端 API 客户端封装
        └── components/         # SidebarTree、DataGridTab、SqlWorkspace、StructureDesigner 等工作台组件
```

---

## English

**Zidong Direct (紫东直连)** is a lightweight, browser-based multi-database management studio built with a **pure-Go backend** and a **React + CodeMirror 6 frontend**. Packaged as a single minimal Alpine Docker container (no JVM/JRE or external Oracle/Dameng native C client required), it provides unified connections, SQL querying, inline data editing, visual table structure design, SSH tunneling, and data import/export across **6 database engines**: MySQL, PostgreSQL, SQLite, SQL Server, Oracle, and Dameng (DM7/DM8).

Its layout draws inspiration from classic desktop clients like Navicat paired with a clean macOS dark frosted-glass aesthetic, while strictly maintaining runtime compatibility with the final browsers supported on **Windows 7 (Chrome 109 / Firefox 115 ESR)**.

### ✨ Key Highlights

- **Single Container, Pure-Go Drivers**: Zero CGO dependencies and lightweight memory footprint compared to Java-based web IDEs.
- **6 Built-in Database Engines**: Native protocol support for MySQL/MariaDB, PostgreSQL, SQLite 3, SQL Server, Oracle (Service Name & SID), and **Dameng (DM7/DM8)**.
- **Legacy / Windows 7 Browser Safe**: Built explicitly targeting `chrome109` & `firefox115` with an automated post-build compatibility gate (`web/scripts/check-win7-compat.mjs`).
- **Desktop-Class Workbench UX**:
  - **Resizable Connection Tree**: Drag the sidebar edge smoothly (`220px`–`680px`, persisted in `localStorage`, double-click to reset). Lazy on-demand node expansion ensures instant startup even on servers with hundreds of tables.
  - **Multi-Tab Workspace & Inspector**: Work across multiple data grids, SQL tabs, and visual schema designers simultaneously, accompanied by a right-hand property, column, and auto-generated `CREATE TABLE` DDL inspector.

### 🗄️ Supported Engines

| Engine | Pure-Go Driver | Default Port | Multi-Schema | SSH Tunnel | Visual DDL Alter | Notes |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| **MySQL / MariaDB** | `go-sql-driver/mysql` | `3306` | - | ✅ | ✅ | UTF8MB4, multi-database browsing, visual structure design |
| **PostgreSQL** | `jackc/pgx/v5` | `5432` | ✅ | ✅ | ✅ | Compatible with PG 9.5–18 (`SCRAM-SHA-256`, `prefer` TLS fallback, PG 12+ `STORED` & PG 18 `VIRTUAL` generated columns) |
| **SQLite 3** | `modernc.org/sqlite` | Local file | - | - | ⚠️ Table Rebuild | Browse hosted dir, upload/download/create `.db` in UI; auto-generates transactional rebuild DDL |
| **SQL Server** | `microsoft/go-mssqldb` | `1433` | ✅ | ✅ | ✅ | Schema browsing, paginated grid, optional `trustServerCert` |
| **Oracle** | `sijms/go-ora/v2` | `1521` | ✅ | ✅ | ✅ | Direct wire protocol (no Instant Client), **Service Name** & **SID** |
| **Dameng (DM7 / DM8)** | `gitee.com/chunanyong/dm` | `5236` | ✅ | ✅ | ✅ | Direct wire protocol, schema/object browsing, CRUD & DDL |

### 🛠️ Features

- **Data Grid & Inline Transactions**: Click-to-sort columns, parameterized multi-condition filters, pagination, double-click inline cell editing, quick `(NULL)` toggle, insert/delete staging (`+ / - / ✓ / ✕`), and transactional commit.
- **SQL Query Workbench**: CodeMirror 6 editor with engine-specific SQL dialect highlighting, table/column autocomplete, `Ctrl+Enter` / `Cmd+Enter` execution, selection-only execution, semicolon statement batching, multi-result tabs, and local query history.
- **Visual Table Designer**: Create or alter tables (columns, type suggestions, PK, auto-increment, defaults, comments, indexes, foreign keys) with live diff DDL preview before execution.
- **SSH Tunneling**: Password or private key authentication (with optional passphrase) and optional SHA256 host key fingerprint verification; automatic tunnel cleanup on disconnect/delete.
- **Import & Export**: Export tables (respecting active filters/sorts) or SQL results to `CSV` (optional UTF-8 BOM for legacy Excel), `JSON`, or `SQL INSERT` scripts; bulk import `CSV` / `JSON` or execute `.sql` files inside a transaction.
- **Security**: Stored database passwords and SSH private keys are encrypted at rest with **AES-256-GCM** and never returned in plaintext via API; optional signed-cookie authentication protects the workbench.

### 🚀 Quick Start with Docker

```bash
git clone https://github.com/Leekinxun/zidong-direct.git
cd zidong-direct
docker compose up -d --build
```

Open `http://<server-ip>:8080` in your browser.

Default login credentials in [`docker-compose.yml`](./docker-compose.yml) (**change `APP_ADMIN_PASSWORD` before exposing to any network**):
- **Username**: `admin`
- **Password**: `changeme123`

#### Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | HTTP server listen port |
| `DATA_DIR` | `/data` | Root directory for encrypted metadata & persisted data |
| `SQLITE_DIR` | `/data/sqlite` | Managed directory for hosted SQLite `.db` files |
| `ALLOW_ARBITRARY_SQLITE_PATHS` | `false` | Allow opening SQLite paths outside `SQLITE_DIR` (disabled by default) |
| `APP_ADMIN_USER` | `admin` | Workbench login username |
| `APP_ADMIN_PASSWORD` | empty (`changeme123` in Compose) | Workbench login password; auth is active when non-empty and `APP_AUTH_DISABLED!=true` |
| `APP_AUTH_DISABLED` | `false` | Set `true` to disable workbench login (trusted local testing only) |
| `APP_SECRET_KEY` | empty | Master encryption/signing key; auto-generated to `/data/meta/.master.key` if unset |
| `MAX_QUERY_ROWS` | `1000` | Maximum rows returned per SQL execution |
| `MAX_UPLOAD_BYTES` | `268435456` (256 MB) | Upload size limit in bytes for SQLite / data imports |
| `QUERY_TIMEOUT_SECONDS` | `60` | Default query execution timeout in seconds |

### 💻 Local Development

**Prerequisites**: Go `1.25+`, Node.js `20+` / `22+`

```bash
# Build frontend & run Win7 compatibility check
cd web
npm ci
npm run build

# Run Go server (serves ../web/dist automatically)
cd ../backend
DATA_DIR=../data APP_AUTH_DISABLED=true go run ./cmd/server
```

For frontend hot-reload dev mode, run `npm run dev` inside `web/` (listens on `:5173` and proxies `/api` to `http://127.0.0.1:8080`).

### 🪟 Windows 7 Browser Compatibility

- Tested/targeted for **Chrome 109** and **Firefox 115 ESR** (or Chromium ≥ 109 derivatives).
- [`web/vite.config.ts`](./web/vite.config.ts) locks Vite `build.target` and `build.cssTarget` to `['chrome109', 'firefox115']`, while Tailwind CSS v3 avoids CSS Color 4 features unavailable in Chrome 109.
- [`web/scripts/check-win7-compat.mjs`](./web/scripts/check-win7-compat.mjs) automatically validates built JS/CSS assets during `npm run build` to block unsupported syntax such as `.toSorted()`, `Object.groupBy()`, `color-mix()`, and `oklch()`.

---

## 📄 License

Released under the [MIT License](./LICENSE).
