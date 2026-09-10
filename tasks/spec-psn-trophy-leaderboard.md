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
| 语言与页面 | Go + 服务端 HTML 模板 | 仓库已是 Go 模块；本地演示不需要前端构建链 |
| HTTP 客户端 | 已有 `github.com/go-resty/resty/v2` | `go.mod` 已引入，不新增同类库 |
| Web 框架 | 标准库 `net/http` | v1 路由少，不引入 Web 框架 |
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

对照 [psn-api](https://github.com/achievements-app/psn-api) 与 [andshrew/PlayStation-Trophies](https://andshrew.github.io/PlayStation-Trophies/#/) 的字段，不把 URL 写死到调用代码以外；行为顺序固定：

1. `PSN_NPSSO` 为空 → `no_credentials`，不访问网络
2. NPSSO 换 access code（`oauth/authorize`，读 302 `Location` 的 `code`），再换 access / refresh token；失败 → `invalid_credentials`
3. access token 进程内缓存，过期前 60 秒刷新；禁止把 NPSSO / token 写入日志或 `error` 字符串
4. `GET .../users/me/profiles`：若 `onlineId` 与目标 ID 大小写相同，accountId 用 `me`（universal search 不会返回当前登录账号）
5. 否则 `POST .../search/v1/universalSearch`，`domain=SocialAllAccounts`，只接受 `onlineId` 精确匹配（忽略大小写）
6. 搜索无精确命中则回退 legacy `.../users/{id}/profile2`；仍无 `accountId` → `not_found`
7. `GET .../trophy/v1/users/{accountId}/trophySummary` 取 `earnedTrophies`；403 → `private`；404 → `not_found`；429 / 5xx / 超时 / 负计数 → `upstream`
8. 积分仍按本地公式计算，不直接采用上游 `trophyPoint`

本地验收命令：`PSN_NPSSO=... go run ./cmd/psnlookup <online-id>`。无凭证时不验收真实网络。

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
- 非官方 PSN 路径若变更，以社区文档现场核对，不锁死 URL 到本 SPEC。

### 11.2 Technical Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| 非官方 PSN 失效 | 真实入榜不可用 | 默认 fixture；错误文案不暴露内部 URL |
| Sony CDN 防盗链 | 头像裂图 | 本地占位图 |
| NPSSO 泄露 | 运营账号风险 | 环境变量、禁止日志 dump、不收用户 token |
| SQLite 锁 | 本地演示几乎不出现 | v1 忽略；高并发是后续 PRD |

### 11.3 Assumptions

- 实现用 Go 1.22 语法；`go.mod` 与本机 toolchain 对齐，不升到当前下不下来的 RC
- 对照文档实现 PSN：[psn-api](https://github.com/achievements-app/psn-api)、[PSNAWP](https://github.com/isFakeAccount/psnawp)、[andshrew/PlayStation-Trophies](https://github.com/andshrew/PlayStation-Trophies)；逐字段核对，不把「看起来能用」当完成
- fixture 内置 3 个 ID（建议 `fixture_alpha` / `fixture_bravo` / `fixture_charlie`），积分必须可区分且测试写死期望值
- 任务临时文件仍按全局规范放 `.tmp/<task-slug>/`，与 SQLite 数据文件分开
- Online ID 大小写不敏感；展示用数据源规范值
- 头像热链允许，失败回退占位图
- 竞赛名次不改为稠密名次，除非产品改 PRD
