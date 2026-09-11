# SPEC: PSN 奖杯排行榜（v1）

> Technical specification derived from: `tasks/prd-psn-trophy-leaderboard.md`
> Living spec — updated 2026-09-11 | Target branch: main | Commit: 4bde179
>
> 本文描述 **已落地的 v1**，不是首版草案。相对 2026-09-10 初稿的架构变更：删除 fixture 与 `PSN_MODE`；`/join` 只走真实 PSN；演示/压测用 `POST /admin/seed`；路由用 go-chi/chi v5；新增 `POST /refresh`；头像为 Sony CDN http 升级 + 首字母回退。
>
> **2026-09-11 增补**：第 13 节增加「高吞吐写入演进架构」设计文档。该设计已批准，待后续独立 PR 实现；当前 main 仍为 v1 同步写入 + 全量排序行为。

## 1. Summary

### 1.1 What This SPEC Covers

本 SPEC 规定 v1 排行榜的实现方式：Go 服务端渲染 HTML、本地 SQLite 持久化、用 resty 调用非官方 PSN 接口。入榜/刷新走真实 PSN（需运营侧 NPSSO）；无凭证时仍可看榜，并用本机 `/admin/seed` 灌模拟用户。产品行为以 PRD 为准；本文只写 how。

### 1.2 PRD Reference

- Source: `tasks/prd-psn-trophy-leaderboard.md`
- User Stories covered: US-001 ~ US-007
- Functional Requirements covered: FR-1 ~ FR-21

### 1.3 Design Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| 语言与页面 | Go + 服务端 HTML 模板 | 仓库已是 Go 模块；本地演示不需要前端构建链 |
| HTTP 客户端 | 已有 `github.com/go-resty/resty/v2` | `go.mod` 已引入，不新增同类库 |
| Web 框架 | go-chi/chi v5 | 轻量路由器，兼容 `net/http` Handler；`Recoverer` 处理 panic |
| 持久化 | SQLite 文件（`modernc.org/sqlite`，纯 Go） | 满足重启不丢数据；v1 不上 Redis / 消息队列 |
| 名次 | 读取时计算，不存 rank 列 | 避免写入后 rank 脏数据 |
| 真实 PSN | 服务端 NPSSO，非官方 trophy API | Sony 无个人可用的官方 OAuth |
| 入榜数据源 | 仅真实 PSN；无 `PSN_MODE` | 演示与压测改走管理灌数，避免假 ID 与真同步混在同一入口 |
| 演示/压测 | `POST /admin/seed`，只接受回环 | 无凭证也能灌榜；不鉴权 token，靠绑本机 |
| 配置 | 环境变量；仓库只提交 `.env.example` | 凭证不入库 |
| 记住「我的排名」 | HttpOnly cookie `online_id` | v1 无本站账号；入榜成功与查找命中均设置 |
| 同步冷却 | `/join` 与 `/refresh` 共用 15 分钟，读 `players.synced_at` | 保护上游；重启后仍生效。seed 写入不走冷却 |
| 积分公式 | 仅 `internal/rank.Score` | 禁止第二套公式 |
| **高吞吐演进架构** | 见第 13 节 | **后续实现**：全量内存 + Top1000 视图 + 门槛双路径 + 10 分片 WAL + 封段刷盘 |

---

## 2. Architecture

### 2.1 System Context

浏览器只访问本服务。`/join`、`/refresh` 由本服务持有运营侧 NPSSO，代表去查目标 Online ID 的公开奖杯汇总。访客不提交密码或 token。`/admin/seed` 不访问 PSN。

**当前架构状态**：v1 基线（PR #1–#10）已升级至高吞吐演进架构（PR #11–#16，Phase 1+2 + 排序优化）。全量内存 + Top1000 视图；双路径写入（热路径同步 SQLite，冷路径 WAL 异步刷盘）；详见 §13。

```
Browser  --HTML form-->  Go HTTP server (chi)
                            |-- memrank (全量内存 + Top1000 视图)
                            |-- WAL (冷路径 10 分片 + 封段刷盘)
                            |-- player store (SQLite 持久化 + 启动加载)
                            |-- trophy.Source → PSN client (resty) --> unofficial PSN API
                            |-- POST /admin/seed (loopback only, no PSN)
```

### 2.2 Component Design

| Component | Responsibility | Must not |
|-----------|----------------|----------|
| `cmd/server` | 读配置、接线、监听 HTTP | 含业务规则 |
| `internal/http` | 路由、表单、模板、cookie、灌数、把领域错误映到中文页 | 直连 PSN |
| `internal/memrank` | 全量内存排行榜 + Top1000 视图；门槛分；线程安全读写 | 直连 PSN |
| `internal/wal` | 10 分片 WAL 文件；封段协议；崩溃恢复重放 | 业务规则 |
| `internal/rank` | 积分、排序、竞赛名次、分页切片 | I/O |
| `internal/player` | 玩家 upsert、按 ID 查、列表读取 | 排名公式 |
| `internal/trophy` | `Source` 接口；PSN 实现（错误码映射） | 写库、记冷却 |
| `internal/psn` | NPSSO 换 token、查用户、拉 trophy summary | 把 token 打进日志 |
| `internal/config` | 读环境变量与 `.env` | 业务逻辑 |

### 2.3 Module Interactions

**热路径写入**（`/join` `/refresh`）：`HTTP` 校验 Online ID → 读 store 做 15 分钟冷却 → `trophy.Source.Lookup` → 校验计数与头像 URL → `rank.Score` → `player.Upsert` 同步 SQLite → `memrank.Upsert` 更新内存 + Top1000 → Set-Cookie → 302 到含 `page` + `highlight` 的榜页。

**冷路径写入**（`/admin/seed`）：回环检查 → 解析 `count` → 生成 `sim`+7 位数字 ID（查重）→ 随机奖杯与 `letter:` 头像 → `rank.Score` → `wal.Append` 追加到分片文件 → 立即返回 JSON（`enqueued`）。后台 Worker 每 100ms 封段刷盘：rename sealed → 读取去重 → 批量 Upsert SQLite → `memrank.UpsertBatch` 更新内存 → 删除 sealed。

**读取榜单**（`GET /`）：`memrank.Page(page, perPage)` → 名次 ≤1000 走 Top1000 视图，之外走全量有序结构 → 按页切片 → 渲染 HTML。稳态读不访问 SQLite。

**查找 / 我的排名**（`/search` `/me`）：校验 ID → `memrank.Get(onlineID)` 从内存 map O(1) 查找 → 存在则用 `indexByID` O(1) 定位名次，302 到对应页并高亮；不存在则 200 渲染「尚未入榜」。查找命中时设置 cookie。

### 2.4 File Structure

```
cmd/psnlookup/main.go           CLI：用 NPSSO 按 Online ID 拉奖杯汇总
cmd/server/main.go              HTTP 服务入口；接线 PSN Source / memrank / WAL
internal/config/config.go       PSN_NPSSO / DB_PATH / LISTEN_ADDR / WAL_DIR / WAL_SEAL_INTERVAL_MS
internal/http/server.go         chi 路由与全部 handler（无独立 handlers.go）
internal/memrank/memrank.go     全量内存排行榜 + Top1000 视图 + 批量更新
internal/wal/wal.go             10 分片 WAL 文件 + 封段刷盘 Worker
internal/rank/rank.go           Score / SortAndNumber / Page（标准库排序，O(N log N)）
internal/player/store.go        SQLite 持久化与批量 Upsert
internal/trophy/source.go       Source 接口与 typed error
internal/trophy/psn.go          PSN Source 包装
internal/psn/client.go          非官方 API
web/templates/leaderboard.html
web/static/app.css
.env.example
README.md
```

无 `internal/trophy/fixture.go`。无 `web/static/placeholder.png`（已删除；空头像用首字母块）。

---

## 3. Data Model

### 3.1 Schema Changes

新建表 `players`（SQLite）：

```sql
CREATE TABLE IF NOT EXISTS players (
    online_id   TEXT PRIMARY KEY COLLATE NOCASE,
    display_id  TEXT NOT NULL,
    avatar_url  TEXT NOT NULL DEFAULT '',
    bronze      INTEGER NOT NULL DEFAULT 0,
    silver      INTEGER NOT NULL DEFAULT 0,
    gold        INTEGER NOT NULL DEFAULT 0,
    platinum    INTEGER NOT NULL DEFAULT 0,
    score       INTEGER NOT NULL DEFAULT 0,
    joined_at   TEXT NOT NULL,
    synced_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_players_score ON players (
    score DESC, platinum DESC, gold DESC, silver DESC, bronze DESC, display_id ASC
);
```

不存 `rank`。时间用 UTC RFC3339 字符串。

`avatar_url` 取值：

- `https://...` — 真实头像（含从索尼 CDN `http://` 升级后的 URL）
- `letter:X` — seed 用户的字母占位（X 为 A–Z）
- 空串 — 模板用 DisplayID 首字母块

### 3.2 Entity Definitions

```go
type Counts struct {
    Bronze, Silver, Gold, Platinum int
}

type Player struct {
    OnlineID  string // 规范化小写，作主键
    DisplayID string // 数据源返回的展示大小写；seed 与 OnlineID 相同
    AvatarURL string
    Counts           // store 用独立 Bronze/Silver/Gold/Platinum 字段
    Score     int
    JoinedAt  time.Time
    SyncedAt  time.Time
}

type RankedPlayer struct {
    Player
    Rank int
}

type Summary struct { // trophy.Summary
    OnlineID, DisplayID, AvatarURL string
    Counts
}
```

### 3.3 Relationships

单表。无用户账号、无游戏、无奖杯明细。

### 3.4 Migration Plan

v1 无独立迁移工具。进程启动时 `CREATE TABLE IF NOT EXISTS`。数据文件默认 `./data/leaderboard.db`（gitignore）。回滚：停进程，删该文件。

---

## 4. API Design

v1 对外是 HTML 页面和表单；`/admin/seed` 是本机 JSON 管理接口，不是公开 JSON API。

### 4.1 Endpoints

| Method | Path | Description | Auth | Request | Response |
|--------|------|-------------|------|---------|----------|
| GET | `/` | 排行榜页 | 无 | `page`、`highlight` | 200 HTML |
| POST | `/join` | 入榜或更新（真实 PSN） | 无 | form `online_id` | 302 或 200 带错误 |
| POST | `/refresh` | 刷新已入榜真实玩家奖杯 | 无 | form `online_id`、`page` | 302 或 200 带错误 |
| GET | `/search` | 按 ID 查找 | 无 | query `online_id` | 302 或 200 带提示 |
| GET | `/me` | 我的排名 | cookie | 无 | 302 或 200 带提示 |
| POST | `/admin/seed` | 追加模拟用户 | 仅回环 | `count`（query 或 form） | JSON |
| GET | `/static/*` | CSS 等 | 无 | 无 | 静态文件 |

默认监听 `127.0.0.1:8080`。README 写 `http://127.0.0.1:8080/` 。路由实现：chi v5；未匹配路径 404；panic 由 `middleware.Recoverer` 回收。

### 4.2 Request/Response Schemas

**GET `/`**

- `page`：正整数，默认 1；非法或 `<1` 当 1
- `highlight`：可选 Online ID，命中则该行加 `is-you` 和「你」标记
- 每页 50 行
- 空库：表格外空态「还没有玩家入榜」
- `page` 超出：表头区域外空态「没有更多玩家」
- 顶栏布局：`>1500px` 入榜/查找/我的排名单行；`769–1500px` 入榜独占第一行；`≤768px` 纵向堆叠

**POST `/join`**

- `application/x-www-form-urlencoded`，字段 `online_id`
- 始终走 `trophy.Source`（生产接线为 PSN）。未配置 `PSN_NPSSO` → 「服务端未配置 PSN 凭证」
- 成功：`Set-Cookie: online_id=<display_id>; Path=/; HttpOnly; SameSite=Lax; Max-Age=2592000`，302 到 `/?page=<n>&highlight=<url-encoded-id>`
- 校验失败 / 同步失败：200 渲染首页，表单下展示对应中文错误，已入榜数据仍显示
- Lookup 使用请求 context，超时 15s；底层 resty 超时 10s

**GET `/search`**

- 在榜：设置与入榜相同的 cookie，302 `/?page=<n>&highlight=<id>`
- 合法但不在榜：200，「该玩家尚未入榜」
- 非法 ID：200，复用入榜校验文案

**GET `/me`**

- 无 cookie 或 cookie 为空：200，「先入榜或先查找」
- 有 cookie：按已记住的 ID 查找；在榜则 302 高亮，不在榜则「该玩家尚未入榜」

**POST `/refresh`**

- `application/x-www-form-urlencoded`，字段 `online_id` 和 `page`
- 必须已在榜：不在榜 → 200，「该玩家尚未入榜」
- 与 `/join` 共享 15 分钟冷却：冷却期内 → 200，「同步过于频繁」
- 成功：与 `/join` 相同的 PSN 同步逻辑（含头像校验、负数拒绝、cookie），302 到 `/?page=<form-page>&highlight=<id>`
- `page` 从 **form** 读取（不是 query）；非法或 `<1` 当 1。模板 hidden 字段带上当前页，避免刷新后掉回第 1 页
- 模拟数据用户（`OnlineID` 前缀 `sim`）：排行榜 UI 不显示刷新按钮

**POST `/admin/seed`**

- 仅回环：`127.0.0.1`、`::1`、`localhost`、`127.0.0.0/8`；否则 403 `{"ok":false,"error":"access denied: seed endpoint only accessible from localhost"}`
- `count`：正整数；缺省/非法/≤0 → 400；**`>1000000`（100万）→ 400**（上限文案含 `1000000`）
- 每次追加，不清空库。ID：`sim` + 7 位定宽十进制（如 `sim0000123`），总长 10；碰撞最多重试 10 次，计入 `failed`
- 奖杯：铜 0–5000、银 0–2000、金 0–800、白金 0–200；积分 `rank.Score`；头像 `letter:` + A–Z
- **Phase 2 后**：追加到 WAL，立即返回 `{"ok":true,"enqueued":N,"failed":M}`（含 `enqueued` 字段表示已入队）；实际写入 SQLite 延迟约 100ms–1s
- **WAL 语义**：`/admin/seed` 返回 200 时，数据已持久化到 WAL 文件（仅剩刷盘窗口 ≤1s 丢失风险）
- **可见延迟**：冷路径用户约 100ms–1s 后可在排行榜查到；热路径（高分冲榜）实时可见

### 4.3 Error Responses

见第 6 节。页面错误用 200 渲染，避免空白 5xx。未处理 panic 由 Recoverer 处理；模板执行失败才是 500。

### 4.4 Breaking Changes

相对 2026-09-10 草案：删除 fixture 入榜与 `PSN_MODE`；新增 `/admin/seed`、`/refresh`。无仓库外既有客户端契约。

---

## 5. Business Logic

### 5.1 Core Algorithms

**积分**

唯一实现：`internal/rank.Score`。

```
score = bronze*15 + silver*30 + gold*90 + platinum*300
```

**排序**

降序：score、platinum、gold、silver、bronze；升序：规范化后的 `online_id`（大小写不敏感）。

**竞赛名次**

遍历排序后列表：第一名 rank=1；与前一名五项计数和 score 全相同则同 rank；否则 rank=当前位置（1-based）。例：1、1、3。

**分页**

`offset = (page-1)*50`。`offset >= len` → 空页 + 「没有更多玩家」。切片 `[offset:min(offset+50,len)]`，行上 rank 为全局名次。

**PSN Lookup（US-006）**

对照 [psn-api](https://github.com/achievements-app/psn-api) 与 [andshrew/PlayStation-Trophies](https://andshrew.github.io/PlayStation-Trophies/#/)；端点、字段与匹配规则以第 12 节为准。行为顺序固定：

1. `PSN_NPSSO` 为空 → `no_credentials`，不访问网络
2. NPSSO 换 access code（`oauth/authorize`，读 302 `Location` 的 `code`），再换 access / refresh token；失败 → `invalid_credentials`
3. access token 进程内缓存，过期前 60 秒刷新；禁止把 NPSSO / token 写入日志或 `error` 字符串
4. `GET .../users/me/profiles`：若返回 400/404（该接口不接受路径 `me`），忽略并继续；若 `onlineId` 与目标 ID 大小写相同，accountId 用 `me`
5. 否则 `POST .../search/v1/universalSearch`，`domain=SocialAllAccounts`。搜索是模糊的，**只接受 `socialMetadata.onlineId` 与目标 ID 忽略大小写后完全相等**；子串相似账号必须丢弃
6. 搜索无精确命中（含 `results: []` 但账号实际存在）则回退 legacy `.../users/{id}/profile2`；仍无 `accountId` → `not_found`
7. `GET .../trophy/v1/users/{accountId}/trophySummary` 取 `earnedTrophies`；403 → `private`；404 → `not_found`；429 / 5xx / 超时 / 负计数 → `upstream`
8. 积分仍按本地公式计算，不直接采用上游 `trophyPoint`
9. v1 **不调用** `trophyTitles` / 单作奖杯列表

本地验收命令：`PSN_NPSSO=... go run ./cmd/psnlookup <online-id>`。无凭证时不验收真实网络。`go run ./cmd/psnlookup --raw <online-id>` 打印非鉴权接口的完整响应 JSON，禁止 dump oauth `/token` 与 NPSSO。

**入榜**

1. Trim 空格；空 → 「请输入 PSN Online ID」，不调数据源
2. 校验格式；失败不调数据源
3. 若该 ID 已在榜且 `synced_at` 距现在 < 15 分钟 → 「同步过于频繁」，保留旧行（seed 用户若被拿去 `/join` 同样受冷却约束）
4. `Source.Lookup`；失败不写库
5. 负计数 → 「暂时无法同步奖杯，请稍后重试」，不写库
6. 头像 URL：见 5.2
7. 已存在：更新 counts、score、avatar、display_id、synced_at，不改 joined_at
8. 不存在：插入，joined_at=synced_at=now
9. 设 cookie，302 到该 ID 所在页

**刷新**（`/refresh`）

1. 校验 Online ID 格式（复用入榜逻辑）
2. 必须已在榜：不在榜 → 200「该玩家尚未入榜」
3. 与 `/join` 共享 15 分钟冷却
4. 成功后走与入榜相同的同步逻辑，302 到 `?page=<form-page>&highlight=<id>`
5. UI：`OnlineID` 不以 `sim` 开头才显示「刷新」

**seed**：不调 PSN，不检查冷却；`synced_at` 仍写成当前时间。

### 5.2 Validation Rules

Online ID：Trim 后长度 3–16，字符集 `[A-Za-z0-9_-]`（允许数字开头）。主键存小写；展示用数据源 `DisplayID`。

奖杯数量与 score 必须 `>= 0`。负值视为数据源错误，走「暂时无法同步奖杯，请稍后重试」。

**头像 URL（入库前）**

1. 空串：接受（页面用 DisplayID 首字母）
2. `https://`：原样接受
3. `http://` 且 host 含 `static-resource.np.community.playstation.net`：改成 `https://` 后入库
4. 其它 `http://`、`javascript:`、`data:`、`ftp:` 等：清空为 `""`
5. 页面：仅 `https://` 走 `<img>`；`letter:` 走字母块；空或加载 `onerror` 用 DisplayID 首字母（大写，空则 `?`）

### 5.3 State Machine

无账号状态机。玩家只有「未入榜 / 已入榜」。已入榜可被 `/join` 或 `/refresh` 更新。seed 用户视为已入榜，但不能从 UI 刷新。

### 5.4 Edge Cases

| Case | Handling |
|------|----------|
| 空库 | 空态，不是错误页 |
| 仅 3 人 | 只有第 1 页；page=2 显示「没有更多玩家」 |
| 完全同分 | 同名次，随后跳号 |
| 仅白金不同 | 白金多者靠前 |
| 重复入榜 | 更新，行数不变 |
| 查找大小写不同 | 命中同一人 |
| 头像 URL 空或加载失败 | DisplayID 首字母圆形块 |
| Sony CDN 返回 http 头像 | 升级为 https 再入库 |
| 真实路径无 NPSSO | 「服务端未配置 PSN 凭证」 |
| PSN 隐私 | 「该用户奖杯未公开，无法入榜」 |
| 搜索返回相似 ID | 只取 `onlineId` 精确匹配（忽略大小写） |
| 搜索 0 条但用户存在 | 走 legacy `profile2` |
| 刷新未入榜 ID | 「该玩家尚未入榜」 |
| 刷新后页码 | 使用 form `page`，不丢当前页 |
| seed 非本机 | 403 JSON |
| seed count=1001 | 400 JSON |

---

## 6. Error Handling

### 6.1 Error Taxonomy

| Error Code | HTTP Status | Condition | User Message |
|------------|-------------|-----------|--------------|
| `id_empty` | 200 | 空提交 | 请输入 PSN Online ID |
| `id_invalid` | 200 | 格式非法 | PSN Online ID 须为 3–16 位字母、数字、连字符或下划线 |
| `not_found` | 200 | 数据源无此用户 | 找不到该 PSN 用户 |
| `private` | 200 | 奖杯未公开 | 该用户奖杯未公开，无法入榜 |
| `no_credentials` | 200 | 未配置凭证 | 服务端未配置 PSN 凭证 |
| `invalid_credentials` | 200 | NPSSO/token 无效 | PSN 凭证无效，请重新获取 NPSSO |
| `upstream` | 200 | 限流、超时、5xx、解析失败、负计数 | 暂时无法同步奖杯，请稍后重试 |
| `cooldown` | 200 | 15 分钟内重复真实同步 | 同步过于频繁 |
| `not_on_board` | 200 | 查找/刷新合法 ID 但不在榜 | 该玩家尚未入榜 |
| `me_missing` | 200 | `/me` 无 cookie | 先入榜或先查找 |

领域错误用 typed error（`trophy.Error`），handler 只做映射。不要把上游响应体写进页面。

`/admin/seed` 不用上表，走 JSON 状态码（403/400/500）。

### 6.2 Retry Strategy

- 入榜/刷新由用户再次提交触发，服务端不对 PSN 自动重试
- resty 超时：10s；handler context：15s；禁止无限重试
- 冷却期内不打上游

### 6.3 Failure Modes

| Dependency | Failure | Degradation |
|------------|---------|-------------|
| SQLite 文件 | 打不开 | 进程启动失败，日志说明路径权限 |
| PSN | 任意错误 | 页面中文错误，旧榜数据仍可读 |
| 头像 CDN | 断裂 / 防盗链 | `<img onerror>` 切首字母块 |
| 未配置 NPSSO | `/join` `/refresh` 失败 | 看榜与 `/admin/seed` 仍可用 |

---

## 7. Security

### 7.1 Authentication & Authorization

v1 无登录。看榜、入榜、查找、刷新均公开。不实现 CSRF token：仅本地演示、无账号可劫持。

`/admin/seed` 无 token，只接受回环地址。`LISTEN_ADDR` 绑到非本机时，调用方仍须是回环；公网暴露自行承担风险。v1 不做 Seed Token。

### 7.2 Input Validation

- Online ID 严格字符集；SQL 只用参数化查询
- 模板必须 HTML escape
- `page` 只解析为整数
- 不接受密码字段；出现也忽略
- 头像 URL 按 5.2 清洗后再入库

### 7.3 Data Protection

- `PSN_NPSSO` 只从环境变量 / `.env` 读，禁止写入仓库、HTML、access log、error 字符串
- resty 日志关闭 body dump
- cookie 存展示用 Online ID，不存 token；HttpOnly + SameSite=Lax；入榜成功与查找命中均设置
- `.env` gitignore；提交 `.env.example`（无真实值；三项：`PSN_NPSSO`、`DB_PATH`、`LISTEN_ADDR`）
- 头像允许热链 Sony CDN；失败用首字母块，不把上游 cookie 当图片参数
- 搜索结果里的 `firstName` / `lastName` / `country` / `language` 不得入库、不得上排行榜；v1 只用 `accountId`、`onlineId`、`avatarUrl`

---

## 7.5 可观测性与性能分析

### 7.5.1 pprof 监控

- **挂载路径**：`/debug/pprof/*` 使用 `net/http/pprof` 标准库包自动注册
- **访问限制**：与主服务共享 `LISTEN_ADDR`，默认 `127.0.0.1:8080`（仅本机可访问）
- **生产部署**：**禁止对公网暴露 pprof**；与 `/admin/seed` 一样靠 `127.0.0.1` 绑定本机
- **用法示例**：
  - CPU profile：`curl http://127.0.0.1:8080/debug/pprof/profile?seconds=30 > cpu.prof`
  - Heap profile：`curl http://127.0.0.1:8080/debug/pprof/heap > heap.prof`
  - Goroutine：`curl http://127.0.0.1:8080/debug/pprof/goroutine > goroutine.txt`
  - 可视化：`go tool pprof -http=:6060 cpu.prof`

### 7.5.2 日志规范

- **统一日志库**：全仓库使用 `log/slog`（Go 1.21+ 标准库结构化日志）
- **输出格式**：默认 `TextHandler` 输出到 `stderr`
- **安全要求**：**禁止把 NPSSO、access token、refresh token 等敏感凭证打进日志**
- **结构化字段**：使用 `slog.String()`、`slog.Int()`、`slog.Duration()` 等附加上下文
- **关键事件**：
  - 启动与初始化（玩家加载数量、内存排行榜门槛分）
  - WAL 封段与刷盘（批次大小、耗时、去重丢弃数）
  - 热路径 vs 冷路径分流决策
  - PSN API 调用失败（不记录响应体完整内容）
  - 错误与异常（含栈信息，但不含敏感字段）

---

## 8. Performance

### 8.1 Expected Load

v1 本地演示：个位数并发、最多数百行。**万级并发写入与秒级可见在演进架构范围**（见第 13 节）。

### 8.2 Optimization Strategy

- 榜单：全表读入内存排序分页。v1 数据量可接受
- 真实 Lookup 冷却 15 分钟（join 与 refresh 共用）
- 静态 CSS 由文件服务，无打包步骤

### 8.3 Database Considerations

- PRIMARY KEY 支持按 ID 更新和查找
- score 复合索引便于以后改 SQL 排序；v1 仍内存排序以保持竞赛名次简单

---

## 9. Testing Strategy

验证命令：`go test ./...`、`go build ./...`。UI 故事需浏览器走一遍。HTTP 测试注入内存 `testSource`，不依赖 fixture 包、不访问外网。

### 9.1 Unit Tests

- `internal/rank`：公式（含全 0）、分差、完全同分、仅白金不同、分页边界、竞赛跳号
- Online ID 校验：空、过短、过长、非法字符、数字开头、合法（CLI `TestValidOnlineID`）
- `internal/trophy`：PSN 错误码映射
- 错误码到文案的映射（HTTP `mapTrophyError`）

### 9.2 Integration Tests

- SQLite 临时文件：upsert 不增行、joined_at 不变、synced_at 变、重启后再读
- HTTP：空榜、入榜 302、非法 ID 不打 Source、查找命中（含 cookie）/未入榜、`/me` 无 cookie
- `/admin/seed`：合法追加、ID 前缀 `sim` 与长度、非法 count、超上限、累加、非回环 403
- `/refresh`：成功、未入榜、冷却、非法 ID、保留 joined_at、form `page` 出现在 Location
- 头像：https 保留；普通 http / ftp / javascript 清空
- 负数计数：不写库
- PSN client：`httptest` fake round-tripper 覆盖 404/隐私/401/429/超时；测试不访问外网

### 9.3 Edge Case Tests

覆盖 5.4 中可自动化部分。真实冷却：HTTP 层读 `synced_at`，第二次不发 Lookup。`PSN_LIVE=1` 为可选外网金样，默认 CI 不跑。

### 9.4 Acceptance Criteria Mapping

| US/FR | Test | Type | Description |
|-------|------|------|-------------|
| US-002 / FR-4,5,6 | `TestScoreAndCompetitionRank` | unit | 公式与跳号 |
| US-003 / FR-7,20 | `TestStoreRestart` / `TestPagination` | integration | 持久化与第 2 页名次 |
| US-004 / FR-10,11,12 | `TestJoinInvalidID` 等 | integration | 校验、更新、redirect |
| US-005 / FR-13 | `TestAdminSeed*` | integration | 灌数、上限、回环、累加 |
| US-006 / FR-14,15,16 | `TestPSNErrorMapping` / HTTP 失败态 | unit | 失败文案互不相同 |
| US-007 / FR-17,18,19 | `TestSearchFound` / `TestMe*` | integration + browser | 查找 cookie 与我的排名 |
| US-001 / FR-1,9 | `TestHomeEmpty` + browser | UI | 空态骨架 |
| FR-3,14 | review + grep | security | 无密码字段；日志无 NPSSO |
| refresh | `TestRefresh*` | integration | 已入榜、冷却、页码 |

---

## 10. Implementation Status

### 10.1 当前状态：Phase 1+2 + 排序优化（已落地）

**最新落地**：2026-09-11 PR #11–#16，完整实现高吞吐写入演进架构 Phase 1+2 及排序优化

#### Phase 1（内存排行榜，PR #12）

- ✅ 启动时从 SQLite `ListAll` 加载全量玩家到内存
- ✅ 维护有序 `ranked` 列表与 **Top1000 视图**
- ✅ 计算门槛分（第 1000 名 score；不足 1000 人时为 −1）
- ✅ 所有**读取**（`GET /`、`/search`、`/me`）：从内存读取，不再每次 `ListAll` + `SortAndNumber`
- ✅ 分页逻辑：名次 ≤1000 走 Top1000 视图；之外走全量有序结构

#### Phase 2（WAL 冷路径，PR #13）

- ✅ 双路径写入：`/join` `/refresh` 热路径（同步 SQLite + 内存）；`/admin/seed` 冷路径（WAL 追加）
- ✅ 10 个 hash 分片文件（`data/wal/shard-N.log`）
- ✅ 封段协议：每 100ms rename 为 sealed → 读取去重 → 批量 Upsert SQLite + 内存 → 删除 sealed
- ✅ 启动时自动重放未处理 sealed 段
- ✅ 硬顶 100 万条/次（PR #14）

#### 可观测性与性能优化（PR #14–#16）

- ✅ **pprof 监控**：挂载到 `/debug/pprof/`，本机访问（PR #14）
- ✅ **slog 日志**：全仓库统一使用 `log/slog`，禁止打印敏感凭证（PR #14）
- ✅ **WAL 锁优化**：封段 flush 仅 rename+创建新文件在锁内，读取+刷盘+内存更新在锁外（PR #14）
- ✅ **批量内存更新**：`UpsertMemoryBatch` 避免逐条 rebuild（PR #15）
- ✅ **排序优化 A**：`rank.SortAndNumber` 使用 `slices.SortFunc`，O(N log N) 替代 O(N²) 插入排序（PR #16）
- ✅ **排序优化 B**：`memrank.UpsertBatch` 使用有序合并，复杂度从 O(M * N log N) 优化为 O(M log M + N)（PR #16）
- ✅ **O(1) 查找**：`indexByID map[string]int` 实现常数时间玩家定位（PR #16）

#### 当前行为总结

- **读路径**：全量内存 + Top1000 视图，稳态读不访问 SQLite
- **热路径写入**：`/join` `/refresh` 同步 SQLite + 实时内存更新
- **冷路径写入**：`/admin/seed` WAL 追加 → 封段刷盘（可见延迟 ≤1s）
- **排序性能**：1 万～10 万量级 rebuild/flush 不再卡在插入排序
- **硬顶限制**：单次 seed 最多 100 万条

#### 未实现（后续）

- Prometheus metrics、反压（§13.7 可观测性建议）
- 热路径 micro-batch（§13.8 Phase 4，可选优化）
- 双路径门槛判断（当前 `/join` `/refresh` 始终热路径，seed 始终冷路径）

### 10.2 v1 基线回顾

v1 已在 main 交付（PR #4–#10，见 `提交历史说明.md`）。实现顺序回顾：

1. `rank` 纯函数 + 测试
2. SQLite store + 测试
3. HTTP 空态页 + CSS
4. 入榜/分页（先 fixture，后改为仅 PSN）
5. 查找、cookie、高亮
6. PSN client + 错误映射 + store 冷却
7. chi 路由；去掉 fixture；`/admin/seed`
8. 头像升级与首字母回退；`/refresh`
9. README、`.env.example`、gitignore 数据文件

未配置凭证时：看榜与 seed 可演示；`/join` `/refresh` 只显示「服务端未配置 PSN 凭证」。

---

## 11. Open Questions & Risks

### 11.1 Unresolved Questions

- 运营侧 NPSSO 是否在演示环境提供？不提供则验收 seed + fake round-tripper + 未配置凭证失败态。
- 第 12 节记录的是 2026-09-10 现场核对过的非官方端点与字段。URL 若变更，以社区文档 + `--raw` 再核对；**匹配规则与取哪些字段**是契约，不随 URL 漂移而放宽。
- **SQLite 批量事务 Upsert**（✅ **已批准方案 B，本 PR 实现**）：WAL flush 改用固定批次 N=100（可做常量/配置），每批一个事务，同批 multi-VALUES upsert。去掉每行前置 SELECT，用 SQL 保留已有 joined_at。只改 WAL flush / ReplaySealed 路径；热路径（`/join` `/refresh`）可继续单条 Upsert。语义：失败保留 sealed 重试不变。

### 11.2 Technical Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| 非官方 PSN 失效 | 真实入榜/刷新不可用 | seed 仍可演示；错误文案不暴露内部 URL |
| Sony CDN 防盗链 | 头像裂图 | 首字母回退；Sony CDN http 升 https |
| NPSSO 泄露 | 运营账号风险 | 环境变量、禁止日志 dump、不收用户 token |
| `/admin/seed` 暴露 | 任意灌数 | 只接受回环；README 警告勿绑公网 |
| SQLite 锁 | 本地演示几乎不出现 | v1 忽略；高并发是后续 PRD |

### 11.3 Assumptions

- 实现用 Go 1.25 语法（`go.mod`：`go 1.25.0`），与 sqlite / x/sys 要求对齐
- 对照文档实现 PSN：[psn-api](https://github.com/achievements-app/psn-api)、[PSNAWP](https://github.com/isFakeAccount/psnawp)、[andshrew/PlayStation-Trophies](https://github.com/andshrew/PlayStation-Trophies)；现场字段以第 12 节为准
- 任务临时文件仍按全局规范放 `.tmp/<task-slug>/`，与 SQLite 数据文件分开
- Online ID 大小写不敏感；展示用数据源规范值
- 竞赛名次不改为稠密名次，除非产品改 PRD
- v1 不拉游戏列表或单枚奖杯；`trophyTitles` 等端点仅文档化，不实现
- v1 不做 Seed Token、不做定时批量刷新、不代理索尼图片

---

## 12. Unofficial PSN API（现场核对）

核对日期：2026-09-10。凭证：运营侧 NPSSO → oauth access token（`--raw` 不打印 token）。现场账号：`cuteCleverDevil`（搜索 0 条，走 legacy）、`Astalosx`（搜索精确命中）。

这些 URL 是非官方、可变的。实现把 base URL 放在 `internal/psn` 的 `Endpoints`，测试用 httptest 替换。

### 12.1 认证（不 dump）

| 步骤 | Method | URL | 要点 |
|------|--------|-----|------|
| NPSSO → code | GET | `https://ca.account.sony.com/api/authz/v3/oauth/authorize` | Cookie `npsso=`；读 302 `Location` 的 `code` |
| code → token | POST | `https://ca.account.sony.com/api/authz/v3/oauth/token` | `grant_type=authorization_code`；响应含 `access_token` / `refresh_token`，禁止日志与 `--raw` |

后续请求：`Authorization: Bearer <access_token>`。超时 10s，禁止自动重试。

### 12.2 解析 Online ID 的调用链

```
GET  .../userProfile/v1/internal/users/me/profiles
        │  400/404 或 onlineId 对不上
        ▼
POST .../search/v1/universalSearch   domain=SocialAllAccounts
        │  无 onlineId 精确匹配（含 results=[]）
        ▼
GET  .../userProfile/v1/users/{onlineId}/profile2
        │  仍无 accountId → not_found
        ▼
GET  .../trophy/v1/users/{accountId}/trophySummary
```

v1 **不调用**（有游戏详情，排行榜用不到）：

| Method | URL | 内容 |
|--------|-----|------|
| GET | `.../trophy/v1/users/{accountId}/trophyTitles` | 每个游戏的名称、平台、该作奖杯数、进度；分页，`limit` 最大约 800 |
| GET | `.../trophy/v1/users/{accountId}/npCommunicationIds/{id}/trophies` | 某一作每一枚奖杯是否已获 |

### 12.3 `GET .../users/me/profiles`

当前索尼不接受路径里的 `accountId=me`。现场：

```json
{
  "error": {
    "code": 2281473,
    "message": "Bad Request (path: accountId)",
    "referenceId": "<uuid>"
  }
}
```

| 字段 | 含义 | v1 |
|------|------|-----|
| `error.code` | 业务错误码。`2281473` = 路径 accountId 非法 | 400/404 时整段忽略，不当作用户不存在 |
| `error.message` | 英文说明 | 不进页面 |
| `error.referenceId` | 索尼侧追踪 ID | 不进页面、不进 error 字符串 |

若未来 200 且 `onlineId` 与目标相同，accountId 用 `"me"`。

### 12.4 `POST .../search/v1/universalSearch`

请求：

```json
{
  "searchTerm": "<online-id>",
  "domainRequests": [{ "domain": "SocialAllAccounts" }]
}
```

**规则：搜索是模糊的。必须 `strings.EqualFold(socialMetadata.onlineId, 目标)` 且 `accountId` 非空才算命中。** 取第一条精确命中即停。禁止用 `score` / `relevancyScore` 选人。

现场 `astalosx`：第一条 `onlineId=Astalosx`（命中），第二条 `XxastalosxX209`（丢弃）。现场 `cutecleverdevil`：`results=[]`，`totalResultCount=0`，继续 legacy。

#### 顶层

| 字段 | 含义 | v1 |
|------|------|-----|
| `domainResponses` | 按搜索域分组的结果 | 只读 `SocialAllAccounts` |
| `fallbackQueried` | 是否走了索尼内部兜底查询 | 忽略 |
| `prefix` | 实际用于前缀匹配的词，通常等于 `searchTerm` | 忽略 |
| `queryFrequency.filterDebounceMs` | 客户端过滤防抖建议（毫秒） | 忽略 |
| `queryFrequency.searchDebounceMs` | 客户端搜索防抖建议 | 忽略 |
| `responseStatus[].status` | 域级 HTTP 风格状态，字符串 `"200"` | 以真正 HTTP 状态为准 |
| `responseStatus[].statusMessage` | 如 `"OK"` | 忽略 |
| `strandPaginationResponse.lastPage` | 是否最后一页 | v1 不翻页 |
| `strandPaginationResponse.offset` | 本页偏移 | 忽略 |
| `strandPaginationResponse.pageSize` | 本页大小 | 忽略 |

#### `domainResponses[]`

| 字段 | 含义 | v1 |
|------|------|-----|
| `domain` | 域 ID，玩家搜索为 `SocialAllAccounts` | 不校验名字，遍历全部域找精确 ID |
| `domainTitle` | 域标题，如「玩家」 | 忽略 |
| `domainTitleHighlight` | 标题高亮分词 | 忽略 |
| `domainTitleMessageId` | 文案 ID，如 `msgid_players` | 忽略 |
| `domainExpandedTitle` | 带关键词的说明，如「名稱類似「astalosx」的玩家」 | 忽略；证明这是相似匹配不是精确查找 |
| `next` | 下一页游标；空表示没有 | 忽略 |
| `results` | 命中列表；可空 | 见下 |
| `totalResultCount` | 本域条数 | 仅观测；0 不代表用户不存在 |
| `zeroState` | 是否空态页 | 忽略 |

#### `results[].socialMetadata`（及同级）

| 字段 | 含义 | v1 |
|------|------|-----|
| `id` | 搜索结果内部 ID（非 PSN accountId） | 忽略 |
| `type` | 如 `social` | 忽略 |
| `score` / `relevancyScore` | 相关度；第一条不必是精确 ID | **禁止当选人依据** |
| `socialMetadata.accountId` | 数字账号 ID，奖杯接口路径参数 | **必用** |
| `socialMetadata.onlineId` | 官方展示用 PSN ID | **精确匹配 + DisplayID** |
| `socialMetadata.avatarUrl` | 头像 URL | 可用 |
| `socialMetadata.profilePicUrl` | 另一套头像（常为 playstation.com 图床） | 忽略；优先 `avatarUrl` |
| `socialMetadata.accountType` | 如 `CUSTOMER` | 忽略 |
| `socialMetadata.country` | 账号地区，如 `HK` | **不入库** |
| `socialMetadata.language` | 界面语言，如 `zh` | **不入库** |
| `socialMetadata.isPsPlus` | 是否 PS Plus | 忽略 |
| `socialMetadata.isOfficiallyVerified` | 是否认证账号 | 忽略 |
| `socialMetadata.verifiedUserName` | 认证名；常为空串 | 忽略 |
| `socialMetadata.firstName` / `lastName` | 搜索里可能出现；现场像是 Online ID 被切开（`Astalosx` → `As` + `talos`），**不能当真实姓名** | **不入库、不上榜** |
| `socialMetadata.highlights.*` | 高亮切分，标明哪一段匹配了搜索词 | 忽略 |

### 12.5 `GET .../users/{onlineId}/profile2`（legacy）

仅搜索无精确命中时调用。Query `fields=npId,onlineId,accountId,avatarUrls,trophySummary(@default,level,progress,earnedTrophies)`。

用户不存在时响应体可带 `error`（仍可能 HTTP 200），视为未命中。

legacy 头像常为 `http://static-resource.np.community.playstation.net/...`。HTTP 层按 5.2 升级为 https 后再入库；奖杯计数仍只信 `trophySummary`。

| 字段 | 含义 | v1 |
|------|------|-----|
| `profile.accountId` | 数字账号 ID | **必用**（有则命中） |
| `profile.onlineId` | 官方大小写 ID | DisplayID |
| `profile.npId` | Base64 形式的内部 npId | 忽略 |
| `profile.avatarUrls[].size` | 尺寸标记，如 `l` | 优先更大尺寸 |
| `profile.avatarUrls[].avatarUrl` | 头像 URL | 可用（http 由 HTTP 层升级） |
| `profile.trophySummary.earnedTrophies.*` | 与新接口同结构的奖杯计数 | **不用这套计数入榜**；仍打 `trophySummary` 新接口，避免两套数源 |
| `profile.trophySummary.level` | 奖杯等级 | 忽略 |
| `profile.trophySummary.progress` | 距下一级百分比 | 忽略 |
| `error.code` / `error.message` | 如用户不存在 | 当未命中 |

### 12.6 `GET .../trophy/v1/users/{accountId}/trophySummary`

排行榜唯一奖杯数据源。路径必须是数字 `accountId`（或认证账号的 `me`），不是 Online ID。

HTTP：403 → `private`；404 → `not_found`；401 → `invalid_credentials`；429/5xx → `upstream`。

| 字段 | 含义 | 现场 `Astalosx` | v1 |
|------|------|-----------------|-----|
| `accountId` | 被查询账号 | `8552327274363529796` | 可校验，不入库为展示字段 |
| `earnedTrophies.bronze` | 已获铜杯总数（全游戏合计） | 21251 | **入榜** |
| `earnedTrophies.silver` | 已获银杯总数 | 7667 | **入榜** |
| `earnedTrophies.gold` | 已获金杯总数 | 3468 | **入榜** |
| `earnedTrophies.platinum` | 已获白金总数 | 731 | **入榜** |
| `trophyLevel` | 奖杯等级 | 838 | 不入库 |
| `tier` | 等级段 1–10（2020 年改制） | 9（金段，约 800–998 级） | 不入库 |
| `progress` | 距下一级百分比 0–100 | 17 | 不入库 |
| `trophyPoint` | 索尼侧总积分 | 1080195 | **不采用**；本地 `铜*15+银*30+金*90+白金*300`。现场两次查询均与该公式相等，仍以本地为准 |
| `trophyLevelBasePoint` | 当前等级起始积分 | 1079640 | 忽略 |
| `trophyLevelNextPoint` | 升到下一级所需积分 | 1082790 | 忽略 |

`earnedTrophies` **没有分游戏**。游戏列表见 12.2 未调用端点。

奖杯等级段（文档值，非索尼保证）：1–3 铜（1–299）、4–6 银（300–599）、7–9 金（600–998）、10 白金（999）。

### 12.7 本工具输出与上游的对应

CLI / 入榜记录只映射：

| 输出 | 来源 |
|------|------|
| Online ID / DisplayID | 精确命中的 `onlineId`（搜索或 legacy） |
| Platinum / Gold / Silver / Bronze | `trophySummary.earnedTrophies` |
| Score | 本地公式 `rank.Score` |
| Avatar | 搜索 `avatarUrl` 或 legacy `avatarUrls`（入库前按 5.2 清洗） |

禁止把 `--raw` 的完整 JSON、token、`firstName`/`lastName`、`country` 写入排行榜库或 HTML。

---

## 13. 高吞吐写入演进架构（已落地：Phase 1+2 + 排序优化）

### 13.1 设计状态与背景

**状态**：本设计已通过技术评审并批准，**Phase 1（内存排行榜）、Phase 2（WAL 冷路径）及排序优化已落地**（PR #11–#16，2026-09-11）。当前 main 实现了：全量内存 + Top1000 视图 + 双路径写入（热/冷）+ WAL 分片封段刷盘 + 标准库排序与有序合并优化。

**批准与实现日期**：2026-09-11（设计）、2026-09-11（Phase 1+2 + 排序优化）

**实现 PR**：
- PR #11: 文档（高吞吐设计 + Mermaid 流程图）
- PR #12: Phase 1（全量内存 + Top1000）
- PR #13: Phase 2（WAL 分片 + 封段刷盘）
- PR #14: 硬顶 100 万 + pprof + slog + 锁优化
- PR #15: UpsertMemoryBatch（批量内存更新）
- PR #16: 排序算法优化 A+B（标准库排序 + 有序合并）

**核心目标**：支撑**约每秒 1 万次**模拟推送/灌数级别的更新操作（注：这不是真实 PSN QPS；真实 PSN 仍受限流与冷却保护），同时保持排行榜**秒级可见性**，并继续使用 **SQLite** 作为持久化方案。

**驱动因素**：

- v1 同步写入 + 每次读取全表排序无法承载万级/秒写入压力
- 需要在压测/演示场景快速构造大规模排行榜数据
- 要求保持当前 SQLite 单机架构，不引入 Redis / 消息队列等外部依赖
- PSN 奖杯**只会增加**，可简化模型（无需处理分数下降导致的榜单维护复杂度）

### 13.2 设计目标与约束

#### 13.2.1 性能目标

| 指标 | 目标值 | 说明 |
|------|--------|------|
| 写入吞吐 | ~10,000 更新/秒 | 主要来自 `/admin/seed` 压测场景；真实 PSN 仍受冷却约束 |
| 可见延迟 | ≤ 1 秒（冷路径） | 热路径实时；冷路径批量刷盘约 100ms，最差 1 秒内可见 |
| 读取延迟 | < 10ms (p99) | Top1000 内存视图；全量内存排序回退 |
| 持久化保证 | 丢失数据 ≤ 1 秒窗口 | 冷路径 WAL 封段间隔与可接受丢失窗口对齐 |

#### 13.2.2 架构约束

- **持久化层**：继续使用 SQLite，不引入 PostgreSQL / MySQL / 分布式数据库
- **运维复杂度**：不引入 Redis ZSET / 消息队列；纯进程内架构
- **分数特性**：PSN 奖杯**只增不减**；设计**不处理**分数下降场景（不考虑掉榜导致的门槛变化）
- **单机部署**：v1 演进阶段保持单进程单机；多副本/分片是后续阶段考虑

### 13.3 核心设计：全量内存 + Top1000 视图 + 双路径写入

#### 13.3.1 内存数据结构

**全量排行数据**：

- 进程启动时从 SQLite 加载所有玩家（`online_id`, `display_id`, `avatar_url`, `bronze`, `silver`, `gold`, `platinum`, `score`, `joined_at`, `synced_at`）
- 维护支持排序的数据结构（例如：Go `map[string]*Player` + 可排序切片，或跳表/红黑树）
- 所有写入（热路径与冷路径刷盘后）必须**同步更新**该内存结构

**Top1000 专用视图**：

- 额外维护前 1000 名玩家的独立有序列表
- 记录**第 1000 名分数**作为「门槛分」（不足 1000 人时门槛视为 −∞，即所有写入走热路径）
- 每次全量内存更新后，重新计算 Top1000 与门槛分
- 查询名次 ≤ 1000 的请求**直接走 Top1000 视图**，避免扫描全量结构

#### 13.3.2 双路径写入：热路径 vs 冷路径

**决策点：新分数是否高于第 1000 名门槛**

```
新更新到达
  ↓
计算 score = bronze*15 + silver*30 + gold*90 + platinum*300
  ↓
score > 门槛分？
  ├─ 是 → 热路径（实时写入）
  └─ 否 → 冷路径（WAL 追加）
```

**热路径（Hot Path）**：

- **触发条件**：新分数 **严格高于** 当前第 1000 名分数；或当前总玩家数 < 1000
- **行为**：
  1. 立即 Upsert SQLite（单条事务）
  2. 更新全量内存结构
  3. 重新计算 Top1000 视图与门槛分
  4. 返回成功（HTTP 302 跳转到榜页并高亮）
- **可见性**：**实时可见**
- **用途**：保证冲榜玩家（高分玩家、真实 PSN 入榜）尽快持久化并可查询

**冷路径（Cold Path）**：

- **触发条件**：新分数 **≤ 门槛分**（排名可能在 1001 名之后，或与第 1000 名同分但字典序靠后）
- **行为**：
  1. 根据 `online_id` 的 hash 值分配到 **10 个 WAL 分片文件**之一（例如：`shard-0.log` ~ `shard-9.log`）
  2. 以追加方式写入事件：`{"online_id":"simXXX","display_id":"simXXX","avatar_url":"...","bronze":100,"silver":50,...,"event_ts":"2026-09-11T12:34:56Z","seq":123456}`
  3. 立即返回成功（或继续下一条写入）
- **可见性**：**延迟约 100ms ~ 1 秒**（取决于下次封段刷盘）
- **用途**：将大部分低分写入从 SQLite 临界区挪走，提升吞吐；适合 `/admin/seed` 批量灌数

**门槛分更新时机**：

- 仅当热路径写入后，重新计算 Top1000；冷路径追加 WAL 时不更新门槛（避免频繁计算）
- 冷路径刷盘后，批量 Upsert 完成，再更新全量内存与 Top1000；此时可能有新玩家进入前 1000 名，门槛分相应变化

#### 13.3.3 WAL 封段与刷盘协议

**封段周期**：约 **100ms**（可配置；与可接受可见延迟对齐）

**对每个分片的操作（10 个 Worker 并发或串行执行）**：

1. **Rename 封段**：
   - 将当前正在写入的 `shard-N.log` **原子 rename** 为不可变段 `shard-N.log.sealed-<timestamp>`（例如 `shard-3.log.sealed-1726056896`）
   - 立即创建新的空 `shard-N.log` 文件供后续写入继续追加
   - **优点**：避免「先备份、再截断原文件」导致的数据窗口问题；rename 是原子操作，崩溃恢复简单
   - **关键**：**Rename 与创建新文件必须在分片锁内完成（快速），封段后立即释放锁**

2. **读取 sealed 段**：
   - **在锁外读取** sealed 段（此时已不可变，无写入竞争）
   - 同一 `online_id` 可能有多条事件（例如模拟灌数时重复更新）；**只保留 `event_ts` 最新的一条**（或用单调递增 `seq` 字段判断）

3. **批量 Upsert SQLite**：
   - **在锁外执行**批量 Upsert 到 SQLite（例如：`INSERT ... ON CONFLICT(online_id) DO UPDATE SET ...`）
   - **✅ 已批准方案 B（本 PR 实现）**：
     - 固定批次 N=100（可做成常量/配置，默认 100）
     - 每组一个事务：`BEGIN` → 该组写入 → `COMMIT`（失败 `ROLLBACK`，保留 sealed 重试语义不变）
     - 同批 multi-VALUES：同一事务内用一条（或少数几条）`INSERT INTO players (...) VALUES (...),(...),... ON CONFLICT(online_id) DO UPDATE SET ...`
     - 去掉每行前置 SELECT joined_at：用 SQL 保留已有 joined_at（`joined_at = players.joined_at` 或等价写法）；新行写 now，冲突行保留旧值
     - 驱动参数上限被迫拆成更小 multi-VALUES 片时，仍须在同一事务内
   - 使用事务减少 fsync 次数

4. **更新内存与 Top1000**：
   - **在锁外更新**全量内存结构（或使用专用内存锁，避免阻塞 WAL 写入）
   - 重新计算 Top1000 视图与门槛分（可能有冷路径玩家因分数增长进入前 1000）
   - **优化**：暴露 `memrank.UpsertBatch(players []Player)` + 单次 `Rebuild()`，避免「每玩家一次全量 SortAndNumber」

5. **删除 sealed 段**：
   - **仅在 SQLite Upsert 成功后**，整文件删除 `shard-N.log.sealed-<timestamp>`
   - 同时更新游标/状态文件，记录「该 sealed 段已成功处理」

6. **崩溃恢复**：
   - 进程重启时，从 SQLite 重建全量内存与 Top1000
   - 扫描所有 `*.log.sealed-*` 文件（幂等重放）：去重后 Upsert SQLite，再更新内存
   - **禁止「先删 WAL 再写库」顺序**（会导致崩溃丢数据）；必须先写库、后删 WAL

**锁优化关键**：

- **持锁范围最小化**：分片锁 **只保护 rename + 创建新文件**（文件系统操作，毫秒级）
- **长耗时操作在锁外**：读 sealed 段、解析 JSON、去重、批量 DB upsert、内存 rebuild 全部在锁外执行
- **避免锁竞争卡住写入**：seed 1 万条时，若每次封段都持锁做「全量 memrank 重排」，会阻塞新写入数秒；改为批量 Upsert 后单次 rebuild
- **内存更新隔离**：memrank 可使用独立的 RWMutex，与 WAL 分片锁分离

**可观测性**：

- 每个分片 QPS、当前 sealed 段积压数量、刷盘耗时（P50/P99）
- 热路径 vs 冷路径写入比例、当前门槛分
- WAL 目录磁盘占用（积压时会增长）

### 13.4 读取路径

#### 13.4.1 排行榜分页（`GET /`）

```
请求 ?page=N
  ↓
计算对应名次范围：[(N-1)*50+1, N*50]
  ↓
名次范围 ≤ 1000？
  ├─ 是 → 直接读 Top1000 视图切片
  └─ 否 → 读全量内存结构排序后切片
  ↓
应用竞赛名次（同分同名次）+ 分页
  ↓
渲染 HTML
```

**优点**：

- 常见场景（前 20 页）只读 Top1000，无需扫描全量
- 全量内存已维护好排序，读取无需每次重新排序（v1 每次 `ListAll` + `SortAndNumber`）

**回退策略**：

- Top1000 视图为空或损坏：回落到全量内存排序（退化为 v1 行为）
- SQLite 连接失败：进程启动失败（与 v1 一致）

#### 13.4.2 查找玩家（`GET /search`）

```
?online_id=XXX
  ↓
在全量内存 map 查找
  ├─ 存在 → 从有序结构定位名次 → 计算所在页 → 302 /?page=N&highlight=XXX
  └─ 不存在 → 200 "该玩家尚未入榜"
```

**无需访问 SQLite**（稳态读）。

#### 13.4.3 我的排名（`GET /me`）

与查找玩家逻辑相同；从 cookie 读取 `online_id` 后走内存查找。

### 13.5 与现有入口的关系

| 入口 | 行为 | 热/冷路径 |
|------|------|-----------|
| `POST /join` | 真实 PSN 入榜；低频（15 分钟冷却） | 默认走热路径（真实玩家通常冲榜；若低分也走冷路径 WAL） |
| `POST /refresh` | 已入榜玩家手动刷新 | 同 `/join`；受冷却保护，频次低 |
| `POST /admin/seed` | 批量灌模拟数据（压测场景） | **主要走冷路径**；随机分数大部分低于门槛；高分灌数走热路径 |
| `GET /` 等读取 | 排行榜查询、分页 | 读 Top1000 视图或全量内存；**不访问 SQLite** |

**注**：`/join` 与 `/refresh` 仍保持同步语义（返回 302 时已可见），因为：

- 真实 PSN 受冷却保护，频次低（15 分钟/次），不会打穿 SQLite
- 用户心理预期「提交后立即看到」，走热路径保证实时可见
- 若未来发现冲击仍大，可增加 micro-batch（见 13.7 风险）

### 13.6 方案对比与选择理由

下表总结讨论过程中评估的备选方案及最终选择本设计的理由：

| 方案 | 优点 | 缺点 | 为何未选/已选 |
|------|------|------|--------------|
| **v1 同步 Upsert + 全量排序** | 实现简单；实时可见；无额外组件 | 写入只有几百/s；每次读取全量 sort | **已落地 v1**；无法支撑 1w/s 写入 |
| **仅队列 + 批量落 SQLite** | 写入吞吐提升到目标量级 | 可见延迟仍高；读路径仍需每次全量排序；未单独优化热点读（前几页） | 写入可达标，但读性能与可见性未解决 |
| **Redis ZSET 热榜** | 吞吐与实时性优秀；排序由 Redis 原生支持 | 新增运维组件；需维护 SQLite ↔ Redis 一致性；多组件故障面 | 当前阶段决定**坚持 SQLite 单机**；后续若需分布式再考虑 |
| **换 Postgres + 分片** | 成熟生态；支持更大规模 | 运维复杂度显著增加；v1 演进阶段非必要 | 下一阶段（多副本/分布式）再评估 |
| **纯内存压测模式（不持久化）** | 吞吐最高；实现最简单 | 数据不持久；不可作为生产方案 | 仅可用作性能基准；不满足「重启不丢数据」需求 |
| **全量内存 + Top1000 + 双路径 WAL**<br/>**（本设计）** | • 1w/s 写入达标（冷路径 WAL 挪出临界区）<br/>• 继续用 SQLite（无新组件）<br/>• 热路径保证冲榜实时可见<br/>• Top1000 优化常见读<br/>• 奖杯只增简化模型<br/>• 封段协议易恢复 | • 冷路径 ≤1s 丢数（可接受）<br/>• 内存占用增加（全量 + Top1000）<br/>• 实现复杂度中等 | **✅ 最终选择**：在约束内达成性能目标；运维复杂度可控；分阶段演进路径清晰 |

**选择理由总结**：

1. **目标吻合**：要 1w/s 级推送 + 仍用 SQLite；本设计通过 WAL 分片 + 封段把大部分写从 SQLite 临界区挪走
2. **读优化**：全量内存保证读/排名本地完成；Top1000 额外视图优化常见前 N 页查询
3. **冲榜保证**：门槛热路径保证高分玩家（真实 PSN、冲榜用户）尽快持久化并可见
4. **简化假设**：奖杯只增不减，设计无需处理降分掉榜、门槛因降分变化等复杂逻辑
5. **崩溃恢复**：封段 rename 协议（vs「备份再抠原文件」）更简单、更易恢复；禁止「先删 WAL 再写库」
6. **可接受妥协**：≤1s 冷路径丢数窗口与封段/刷盘周期对齐；对于演示/压测场景可接受

### 13.7 风险、限制与运维注意事项

#### 13.7.1 已识别风险

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| **sealed 段积压** | WAL 目录体积膨胀；刷盘延迟增加 | • 监控每片 sealed 数量与目录体积<br/>• 设置积压阈值，触发反压（拒绝新写入或降级）<br/>• 后续可增加更多 Worker 并行刷盘 |
| **热路径被随机高分打穿** | 大量灌数时若随机高分过多，仍可能冲击 SQLite | • 本设计接受该风险；先实现观测<br/>• **后续 follow-up**：热路径增加 micro-batch（聚合 10ms 窗口内热写入再批量 Upsert）<br/>• 不阻塞本设计文档与初版实现 |
| **内存占用** | 全量 + Top1000 双份内存；百万玩家约数百 MB | • v1 演进阶段可接受（本地演示/压测规模）<br/>• 后续若需支撑千万级，考虑分片或仅 Top-N 内存 |
| **旧事件覆盖新状态** | 刷盘时若事件乱序，旧 seq 覆盖新 seq | • 去重时必须保留 `event_ts` 最新或 `seq` 最大的事件<br/>• SQLite Upsert 前检查 `synced_at`；旧时间戳不覆盖新记录 |
| **崩溃恢复时间** | 百万玩家重建内存 + 重放积压 sealed 可能数十秒 | • 启动健康检查；流量接入前完成恢复<br/>• 后续可增加内存快照（定期 dump）加速启动 |

#### 13.7.2 可观测性（Metrics & Logs）

实现时必须暴露以下指标：

- **写入 QPS**：`write_qps_total`、`write_qps_hot`、`write_qps_cold`（按路径分）
- **WAL 状态**：`wal_shard_sealed_count{shard="N"}`（每片积压段数）、`wal_dir_size_bytes`（目录总大小）
- **刷盘性能**：`wal_flush_duration_seconds{shard="N",quantile="0.5|0.99"}`、`wal_flush_batch_size{shard="N"}`
- **Top1000**：`top1000_threshold_score`（当前门槛分）、`top1000_update_duration_seconds`
- **热路径 SQLite**：`hot_path_sqlite_upsert_duration_seconds{quantile}`
- **内存重建**：`memory_rebuild_duration_seconds`（启动时）、`memory_player_count`

日志关键事件：

- 封段成功/失败、刷盘批次大小、去重丢弃事件数
- 门槛分变化（`threshold changed: 12345 -> 12678`）
- sealed 段积压超阈值告警

#### 13.7.3 配置参数（建议）

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `WAL_SHARD_COUNT` | 10 | WAL 分片数量；增加可分散写入热点，但增加 Worker 数 |
| `WAL_SEAL_INTERVAL_MS` | 100 | 封段周期（毫秒）；与可见延迟对齐 |
| `WAL_DIR` | `./data/wal/` | WAL 文件目录 |
| `WAL_MAX_SEALED_PER_SHARD` | 100 | 每片最大积压段数；超过触发反压 |
| `TOP_N_SIZE` | 1000 | Top-N 视图大小；可配置为 500/2000 等 |
| `MEMORY_SNAPSHOT_INTERVAL` | 0（禁用） | 内存快照周期（秒）；后续优化启动速度 |

### 13.8 实现里程碑（建议分阶段 PR）

本设计文档不涵盖实现细节；以下为建议实现拆分（供后续实施 PR 参考）：

1. **Phase 1**：全量内存 + Top1000 视图（无 WAL）✅ **已落地（2026-09-11 PR #12）**
   - 启动时加载 SQLite 到内存；维护 Top1000
   - 所有写入仍同步 Upsert SQLite + 更新内存
   - 读路径改为读 Top1000 / 全量内存，不再每次 `ListAll` + `SortAndNumber`
   - **验收**：读性能提升；写入仍为 v1 水平
   - **实现模块**：`internal/memrank` 封装内存排行榜；HTTP 层集成
   
   **Phase 1+ 排序优化** ✅ **已落地（2026-09-11 PR #16）**
   - **优化 A**：`internal/rank.SortAndNumber` 使用标准库 `slices.SortFunc`，O(N log N) 替代 O(N²) 插入排序
   - **优化 B**：`internal/memrank.UpsertBatch` 使用有序合并（ordered merge）而非全量重排序
     - WAL flush 批量更新时：移除旧项 → 排序批次 → 二路归并 → 重新分配竞赛名次
     - 复杂度从 O(M * N log N) 优化为 O(M log M + N)（批量 M，总量 N）
   - 新增 `indexByID map[string]int` 实现 O(1) 查找，优化 `Get` 和 `IndexOf`
   - 新增大数据量测试（1k-50k）确保性能改善
   - **验收**：1 万～10 万量级 rebuild/flush 不再卡在插入排序；所有测试通过

2. **Phase 2**：冷路径 WAL + 封段刷盘 ✅ **已落地（2026-09-11 PR #13）**
   - 去除 `/admin/seed` count 上限（仍须正整数）
   - 模拟数据追加到 10 个 hash 分片文件（`data/wal/shard-N.log`）
   - 封段 Worker：每 100ms rename 为 sealed，读取去重，批量 Upsert SQLite + 内存，删除 sealed
   - 启动时重放未处理 sealed 段
   - `/join` `/refresh` 仍同步写库（热路径），seed 走 WAL（冷路径）
   - **验收**：`/admin/seed` 支持大批量灌数（如 10000+）；入队即返回 JSON（enqueued/failed）；可见延迟约 100ms-1s

   **Phase 2+ WAL 批事务刷盘优化** ⏳ **本 PR 实现（已批准方案 B）**
   - WAL flush 路径改用批事务 + multi-VALUES（固定批次 N=100，可配置）
   - 每批一个事务：`BEGIN` → multi-VALUES upsert → `COMMIT`（失败 `ROLLBACK`，保留 sealed 重试语义）
   - 去掉每行前置 SELECT joined_at：用 SQL `ON CONFLICT ... DO UPDATE SET joined_at = players.joined_at` 保留已有值
   - 只改 WAL flush / ReplaySealed 路径；热路径（`/join` `/refresh`）可继续单条 Upsert
   - **验收**：大批量 flush（如 1000+ 条）时，I/O 与事务开销显著降低；所有测试通过

3. **Phase 3**：Prometheus metrics + 反压（待实现）
   - 暴露 Prometheus metrics（写入 QPS、WAL 状态、刷盘性能、Top1000 门槛分等，见 §13.7.2）
   - 增加反压与告警（sealed 段积压超阈值触发反压）
   - **验收**：监控面板可用，反压机制生效

4. **Phase 4（可选 follow-up）**：热路径 micro-batch（待实现）
   - 若观测到热路径仍有冲击，增加 10ms 窗口聚合
   - **验收**：热路径 QPS 进一步提升

### 13.9 文档更新计划

本设计落地后，需更新以下 SPEC 章节：

- **第 2 节 Architecture**：增加内存层与 WAL Worker 组件图
- **第 5 节 Business Logic**：更新入榜/seed 逻辑走双路径判断
- **第 8 节 Performance**：删除「上万并发不在范围」；增加新性能目标
- **第 10 节 Implementation Status**：标记本架构已落地；v1 行为迁移到「历史版本」子节

**本次文档 PR** 不删除现有 v1 描述；只**增补**本节（第 13 节）说明演进设计，并在相关章节增加「见第 13 节」引用。
