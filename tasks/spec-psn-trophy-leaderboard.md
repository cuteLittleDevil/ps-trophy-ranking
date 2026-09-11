# SPEC: PSN 奖杯排行榜（v1）

> Technical specification derived from: `tasks/prd-psn-trophy-leaderboard.md`
> Generated: 2026-09-10 | Target branch: main | Commit: 2f300c4

## 1. Summary

### 1.1 What This SPEC Covers

本 SPEC 规定 v1 排行榜的实现方式：Go 服务端渲染 HTML、本地 SQLite 持久化、用已有 resty 调用非官方 PSN 接口、演示数据（fixture）与真实 PSN 可切换。产品行为以 PRD 为准；本文只写 how。

### 1.2 PRD Reference

- Source: `/Users/chendon/GolandProjects/tmp/ps-trophy-ranking/tasks/prd-psn-trophy-leaderboard.md`
- User Stories covered: US-001 ~ US-007
- Functional Requirements covered: FR-1 ~ FR-21

### 1.3 Design Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| 语言与页面 | Go + 服务端 HTML 模板 | 仓库已是 Go 模块;本地演示不需要前端构建链 |
| HTTP 客户端 | 已有 `github.com/go-resty/resty/v2` | `go.mod` 已引入，不新增同类库 |
| Web 框架 | go-chi/chi v5 | 轻量 HTTP 路由器，兼容 `net/http` Handler，路由少但更清晰 |
| 持久化 | SQLite 文件 | 满足重启不丢数据；v1 不上 Redis / 消息队列 |
| 名次 | 读取时计算，不存 rank 列 | 避免写入后 rank 脏数据 |
| 真实 PSN | 服务端 NPSSO，非官方 trophy API | Sony 无个人可用的官方 OAuth |
| 配置 | 环境变量；仓库只提交 `.env.example` | 凭证不入库 |
| 记住「我的排名」 | HttpOnly cookie `online_id` | v1 无本站账号 |
| 默认数据源 | `PSN_MODE=fixture` | 无凭证也能演示 |
| 同步冷却 | 真实模式 15 分钟；fixture 关闭 | 保护上游，演示不受阻 |

---

## 2. Architecture

### 2.1 System Context

浏览器只访问本服务。服务在入榜时按配置走 fixture 或 PSN。PSN 路径由本服务持有运营侧 NPSSO，代表去查目标 Online ID 的公开奖杯汇总。访客不提交密码或 token。

```
Browser  --HTML form-->  Go HTTP server
                            |-- ranking (pure)
                            |-- player store (SQLite)
                            |-- trophy source
                                  |-- fixture (in-process)
                                  |-- PSN client (resty) --> unofficial PSN API
```

### 2.2 Component Design

| Component | Responsibility | Must not |
|-----------|----------------|----------|
| `cmd/server` | 读配置、接线、监听 HTTP | 含业务规则 |
| `internal/http` | 路由、表单、模板、cookie、把领域错误映到中文页 | 直连 PSN |
| `internal/rank` | 积分、排序、竞赛名次、分页切片 | I/O |
| `internal/player` | 玩家 upsert、按 ID 查、列表读取 | 排名公式 |
| `internal/trophy` | `Source` 接口；fixture / psn 实现 | 写库 |
| `internal/psn` | NPSSO 换 token、查用户、拉 trophy summary | 把 token 打进日志 |

### 2.3 Module Interactions

入榜：`HTTP` 校验 Online ID → 冷却检查 → `trophy.Source.Lookup` → 算分 → `player.Upsert` → 重定向到含 `page` + `highlight` 的榜页。

看榜：`player.ListAll` → `rank.SortAndNumber` → 按页切片 → 渲染。

查找 / 我的排名：校验 ID → 在已排序列表定位 → 存在则 302 到对应页并高亮；不存在则 200 渲染「尚未入榜」。

### 2.4 File Structure

```
cmd/psnlookup/main.go           [NEW: 用 NPSSO 按 Online ID 拉奖杯汇总]
cmd/server/main.go              [NEW]
internal/config/config.go       [NEW]
internal/http/server.go         [NEW]
internal/http/handlers.go       [NEW]
internal/rank/rank.go           [NEW]
internal/rank/rank_test.go      [NEW]
internal/player/store.go        [NEW]
internal/player/store_test.go   [NEW]
internal/trophy/source.go       [NEW]
internal/trophy/fixture.go      [NEW]
internal/trophy/fixture_test.go [NEW]
internal/psn/client.go          [NEW]
internal/psn/client_test.go     [NEW]
web/templates/leaderboard.html  [NEW]
web/static/app.css              [NEW]
.env.example                    [NEW]
README.md                       [MODIFY: 启动、fixture、可选 PSN 凭证]
```

---

## 3. Data Model

### 3.1 Schema Changes

新建表 `players`（SQLite）：

```sql
CREATE TABLE players (
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

CREATE INDEX idx_players_score ON players (score DESC, platinum DESC, gold DESC, silver DESC, bronze DESC, display_id ASC);
```

不存 `rank`。时间用 UTC RFC3339 字符串。

### 3.2 Entity Definitions

```go
type Counts struct {
    Bronze, Silver, Gold, Platinum int
}

type Player struct {
    OnlineID  string // 规范化小写，作主键
    DisplayID string // 数据源返回的展示大小写
    AvatarURL string
    Counts
    Score     int
    JoinedAt  time.Time
    SyncedAt  time.Time
}

type RankedPlayer struct {
    Player
    Rank int
}

type TrophySummary struct {
    OnlineID  string
    DisplayID string
    AvatarURL string
    Counts
}
```

### 3.3 Relationships

单表。无用户账号、无游戏、无奖杯明细。

### 3.4 Migration Plan

v1 无独立迁移工具。进程启动时 `CREATE TABLE IF NOT EXISTS`。数据文件默认 `./data/leaderboard.db`（gitignore）。回滚：停进程，删该文件。

---

## 4. API Design

v1 对外是 HTML 页面和表单，不是 JSON API。

### 4.1 Endpoints

| Method | Path | Description | Auth | Request | Response |
|--------|------|-------------|------|---------|----------|
| GET | `/` | 排行榜页 | 无 | `page`、`highlight` | 200 HTML |
| POST | `/join` | 入榜或更新 | 无 | form `online_id` | 302 或 200 带错误 |
| GET | `/search` | 按 ID 查找 | 无 | query `online_id` | 302 或 200 带提示 |
| GET | `/me` | 我的排名 | cookie | 无 | 302 或 200 带提示 |
| GET | `/static/*` | CSS 等 | 无 | 无 | 静态文件 |

默认监听 `127.0.0.1:8080`。README 写 `http://127.0.0.1:8080/` 。

### 4.2 Request/Response Schemas

**GET `/`**

- `page`：正整数，默认 1；非法或 `<1` 当 1
- `highlight`：可选 Online ID，命中则该行加 `is-you` 和「你」标记
- 每页 50 行
- 空库：表格表头 + 「还没有玩家入榜」
- `page` 超出：表头 + 「没有更多玩家」

**POST `/join`**

- `application/x-www-form-urlencoded`，字段 `online_id`
- 成功：`Set-Cookie: online_id=<display_id>; Path=/; HttpOnly; SameSite=Lax; Max-Age=2592000`，302 到 `/?page=<n>&highlight=<url-encoded-id>`
- 校验失败 / 同步失败：200 渲染首页，表单下展示对应中文错误，已入榜数据仍显示

**GET `/search`**

- 在榜：302 `/?page=<n>&highlight=<id>`
- 合法但不在榜：200，「该玩家尚未入榜」
- 非法 ID：200，复用入榜校验文案

**GET `/me`**

- 无 cookie：200，「先入榜或先查找」
- 有 cookie：按 `/search` 处理

### 4.3 Error Responses

见第 6 节。页面错误用 200 渲染，避免空白 5xx。未处理 panic 才是 500。

### 4.4 Breaking Changes

无既有对外 API。

---

## 5. Business Logic

### 5.1 Core Algorithms

**积分**

```
score = bronze*15 + silver*30 + gold*90 + platinum*300
```

**排序**

降序：score、platinum、gold、silver、bronze；升序：`display_id`（字节序，大小写不敏感比较时用已规范化的 `online_id`）。

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
5. 否则 `POST .../search/v1/universalSearch`，`domain=SocialAllAccounts`。搜索是模糊的，**只接受 `socialMetadata.onlineId` 与目标 ID 忽略大小写后完全相等**；子串相似账号（如搜 `astalosx` 得到 `XxastalosxX209`）必须丢弃
6. 搜索无精确命中（含 `results: []` 但账号实际存在，如 `cutecleverdevil`）则回退 legacy `.../users/{id}/profile2`；仍无 `accountId` → `not_found`
7. `GET .../trophy/v1/users/{accountId}/trophySummary` 取 `earnedTrophies`；403 → `private`；404 → `not_found`；429 / 5xx / 超时 / 负计数 → `upstream`
8. 积分仍按本地公式计算，不直接采用上游 `trophyPoint`（现场值目前与公式一致，但不能当契约）
9. v1 **不调用** `trophyTitles` / 单作奖杯列表；排行榜只要全账号汇总

本地验收命令：`PSN_NPSSO=... go run ./cmd/psnlookup <online-id>`。无凭证时不验收真实网络。`go run ./cmd/psnlookup --raw <online-id>` 打印非鉴权接口的完整响应 JSON，禁止 dump oauth `/token` 与 NPSSO。

**入榜**

1. Trim 空格；空 → 「请输入 PSN Online ID」，不调数据源
2. 校验格式；失败不调数据源
3. 真实模式：若该 ID 的 `synced_at` 距现在 < 15 分钟 → 「同步过于频繁」，保留旧行
4. `Source.Lookup`；失败不写库
5. 已存在：更新 counts、score、avatar、display_id、synced_at，不改 joined_at
6. 不存在：插入，joined_at=synced_at=now
7. 设 cookie，302 到该 ID 所在页

**fixture 冷却**：关闭（每次提交都更新）。

### 5.2 Validation Rules

Online ID：Trim 后长度 3–16，字符集 `[A-Za-z0-9_-]`。主键存小写；展示用数据源 `DisplayID`，fixture 用输入原样（去空格）。

奖杯数量与 score 必须 `>= 0`。负值视为数据源错误，走「暂时无法同步奖杯，请稍后重试」。

### 5.3 State Machine

无账号状态机。玩家只有「未入榜 / 已入榜」。已入榜可被更新。

### 5.4 Edge Cases

| Case | Handling |
|------|----------|
| 空库 | 空态，不是错误页 |
| 仅 3 人 | 只有第 1 页；page=2 显示「没有更多玩家」 |
| 完全同分 | 同名次，随后跳号 |
| 仅白金不同 | 白金多者靠前 |
| 重复入榜 | 更新，行数不变 |
| 查找大小写不同 | 命中同一人 |
| 头像 URL 空或加载失败 | 本地占位图 |
| fixture 未知 ID | 「找不到该 PSN 用户」 |
| 真实模式无 NPSSO | 「服务端未配置 PSN 凭证」 |
| PSN 隐私 | 「该用户奖杯未公开，无法入榜」 |
| 搜索返回相似 ID | 只取 `onlineId` 精确匹配（忽略大小写） |
| 搜索 0 条但用户存在 | 走 legacy `profile2` |

---

## 6. Error Handling

### 6.1 Error Taxonomy

| Error Code | HTTP Status | Condition | User Message |
|------------|-------------|-----------|--------------|
| `id_empty` | 200 | 空提交 | 请输入 PSN Online ID |
| `id_invalid` | 200 | 格式非法 | PSN Online ID 须为 3–16 位字母、数字、连字符或下划线 |
| `not_found` | 200 | 数据源无此用户 | 找不到该 PSN 用户 |
| `private` | 200 | 奖杯未公开 | 该用户奖杯未公开，无法入榜 |
| `no_credentials` | 200 | 真实模式未配置凭证 | 服务端未配置 PSN 凭证 |
| `upstream` | 200 | 限流、超时、5xx、解析失败 | 暂时无法同步奖杯，请稍后重试 |
| `cooldown` | 200 | 15 分钟内重复真实同步 | 同步过于频繁 |
| `not_on_board` | 200 | 查找合法 ID 但不在榜 | 该玩家尚未入榜 |
| `me_missing` | 200 | `/me` 无 cookie | 先入榜或先查找 |

领域错误用 typed error，handler 只做映射。不要把上游响应体写进页面。

### 6.2 Retry Strategy

- 入榜由用户再次提交触发，服务端不对 PSN 自动重试
- resty 超时：10s，禁止无限重试
- 冷却期内不打上游

### 6.3 Failure Modes

| Dependency | Failure | Degradation |
|------------|---------|-------------|
| SQLite 文件 | 打不开 | 进程启动失败，日志说明路径权限 |
| PSN | 任意错误 | 页面中文错误，旧榜数据仍可读 |
| 头像 CDN | 断裂 | `<img>` `onerror` 切占位图 |
| fixture | 未知 ID | `not_found`，服务仍可用 |

---

## 7. Security

### 7.1 Authentication & Authorization

v1 无登录。看榜、入榜、查找均公开。不实现 CSRF token：仅本地演示、无账号可劫持。

### 7.2 Input Validation

- Online ID 严格字符集，拒绝注入到 SQL：只用参数化查询
- 模板必须 HTML escape
- `page` 只解析为整数
- 不接受密码字段；出现也忽略

### 7.3 Data Protection

- `PSN_NPSSO` 只从环境变量读，禁止写入仓库、HTML、access log、error 字符串
- resty 日志关闭 body dump
- cookie 存展示用 Online ID，不存 token；HttpOnly + SameSite=Lax
- `.env` gitignore；提交 `.env.example`（无真实值）
- 头像允许热链 Sony CDN；失败用本地占位图，不把上游 cookie 当图片参数
- 搜索结果里的 `firstName` / `lastName` / `country` / `language` 不得入库、不得上排行榜；v1 只用 `accountId`、`onlineId`、`avatarUrl`

---

## 8. Performance

### 8.1 Expected Load

v1 本地演示：个位数并发、最多数百行。上万并发不在范围。

### 8.2 Optimization Strategy

- 榜单：全表读入内存排序分页。v1 数据量可接受
- 真实 Lookup 冷却 15 分钟
- 静态 CSS 由文件服务，无打包步骤

### 8.3 Database Considerations

- PRIMARY KEY 支持按 ID 更新和查找
- score 复合索引便于以后改 SQL 排序；v1 仍可内存排序以保持竞赛名次简单

---

## 9. Testing Strategy

验证命令：`go test ./...`、`go build ./...`。UI 故事需浏览器走一遍。

### 9.1 Unit Tests

- `internal/rank`：分差、完全同分、仅白金不同、全 0、分页边界、竞赛跳号
- Online ID 校验：空、过短、过长、非法字符、合法
- fixture：3 个内置 ID 数字固定；未知 ID → `not_found`
- 错误码到文案的映射表

### 9.2 Integration Tests

- SQLite 临时文件：upsert 不增行、joined_at 不变、synced_at 变、重启后再读
- HTTP：空榜、入榜 302、非法 ID 不打 Source、查找命中/未入榜、`/me` 无 cookie
- PSN client：`httptest` fake round-tripper 覆盖 404/隐私/401/429/超时；测试不访问外网

### 9.3 Edge Case Tests

覆盖 5.4。真实模式冷却：同一 ID 两次 Lookup，第二次不发 HTTP。

### 9.4 Acceptance Criteria Mapping

| US/FR | Test | Type | Description |
|-------|------|------|-------------|
| US-002 / FR-4,5,6 | `TestScoreAndCompetitionRank` | unit | 公式与跳号 |
| US-003 / FR-7,20 | `TestStoreRestartAndPageOffset` | integration | 持久化与第 2 页名次 |
| US-004 / FR-10,11,12 | `TestJoinValidationAndUpsert` | integration | 校验、更新、redirect |
| US-005 / FR-13 | `TestFixtureJoin` | unit + browser | 默认 fixture，3 个假玩家 |
| US-006 / FR-14,15,16 | `TestPSNErrorMapping` | unit | 五类失败文案互不相同 |
| US-007 / FR-17,18,19 | `TestSearchAndMeCookie` | integration + browser | 查找与我的排名 |
| US-001 / FR-1,9 | browser | UI | 空态骨架 |
| FR-3,14 | review + grep | security | 无密码字段；日志无 NPSSO |

---

## 10. Implementation Plan

### 10.1 Phases

1. `rank` 纯函数 + 测试
2. SQLite store + 测试
3. HTTP 空态页 + CSS
4. fixture Source + 入榜/分页
5. 查找、cookie、高亮
6. PSN client + 错误映射 + 冷却
7. README、`.env.example`、gitignore 数据文件

### 10.2 Issue Mapping

| Issue | SPEC Sections | Priority | Depends On |
|-------|--------------|----------|------------|
| US-002 积分与名次 | 5.1, 9.1 | high | — |
| US-001 空态页 | 2.4, 4.1 | high | — |
| US-003 持久化分页 | 3, 5.1 | high | US-002 |
| US-005 fixture | 2.2, 5.1 | high | US-003 |
| US-004 入榜校验更新 | 4.2, 5.1, 5.2 | high | US-005 |
| US-007 查找与我的排名 | 4.2, 7.3 | medium | US-004 |
| US-006 真实 PSN 失败态 | 6, 7.3 | medium | US-004 |

### 10.3 Incremental Delivery

默认 fixture，全程可演示。PSN 为可选增强：未配置凭证时真实模式只显示「服务端未配置 PSN 凭证」，不影响看榜。

---

## 11. Open Questions & Risks

### 11.1 Unresolved Questions

- 运营侧 NPSSO 是否在 v1 提供？不提供则只验收 fixture 与 fake round-tripper。
- 第 12 节记录的是 2026-09-10 现场核对过的非官方端点与字段。URL 若变更，以社区文档 + `--raw` 再核对；**匹配规则与取哪些字段**是契约，不随 URL 漂移而放宽（尤其禁止把搜索模糊命中当精确用户）。

### 11.2 Technical Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| 非官方 PSN 失效 | 真实入榜不可用 | 默认 fixture；错误文案不暴露内部 URL |
| Sony CDN 防盗链 | 头像裂图 | 本地占位图 |
| NPSSO 泄露 | 运营账号风险 | 环境变量、禁止日志 dump、不收用户 token |
| SQLite 锁 | 本地演示几乎不出现 | v1 忽略；高并发是后续 PRD |

### 11.3 Assumptions

- 实现用 Go 1.22 语法；`go.mod` 与本机 toolchain 对齐，不升到当前下不下来的 RC
- 对照文档实现 PSN：[psn-api](https://github.com/achievements-app/psn-api)、[PSNAWP](https://github.com/isFakeAccount/psnawp)、[andshrew/PlayStation-Trophies](https://github.com/andshrew/PlayStation-Trophies)；逐字段核对，不把「看起来能用」当完成。现场字段以第 12 节为准
- fixture 内置 3 个 ID（建议 `fixture_alpha` / `fixture_bravo` / `fixture_charlie`），积分必须可区分且测试写死期望值
- 任务临时文件仍按全局规范放 `.tmp/<task-slug>/`，与 SQLite 数据文件分开
- Online ID 大小写不敏感；展示用数据源规范值
- 头像热链允许，失败回退占位图
- 竞赛名次不改为稠密名次，除非产品改 PRD
- v1 不拉游戏列表或单枚奖杯；`trophyTitles` 等端点仅文档化，不实现

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

| 字段 | 含义 | v1 |
|------|------|-----|
| `profile.accountId` | 数字账号 ID | **必用**（有则命中） |
| `profile.onlineId` | 官方大小写 ID | DisplayID |
| `profile.npId` | Base64 形式的内部 npId | 忽略 |
| `profile.avatarUrls[].size` | 尺寸标记，如 `l` | 优先更大尺寸 |
| `profile.avatarUrls[].avatarUrl` | 头像 URL | 可用 |
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
| Score | 本地公式 |
| Avatar | 搜索 `avatarUrl` 或 legacy `avatarUrls` |

禁止把 `--raw` 的完整 JSON、token、`firstName`/`lastName`、`country` 写入排行榜库或 HTML。
