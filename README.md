# ps-trophy-ranking

PSN 奖杯用户排行榜。v1 排行榜页面已完成，支持演示数据与真实 PSN 档案切换。

## 启动排行榜服务

在项目根目录：

```bash
go run ./cmd/server
```

默认监听 `http://127.0.0.1:8080`，浏览器打开即可看到排行榜页面。

## 演示步骤（默认模式）

默认使用演示数据，无需配置 PSN 凭证即可体验完整功能：

1. 启动服务：`go run ./cmd/server`
2. 浏览器打开 `http://127.0.0.1:8080`
3. 在「加入排行榜」表单输入以下任一 ID：
   - `fixture_alpha`（白金 50、金 200、银 500、铜 1000）
   - `fixture_bravo`（白金 40、金 150、银 400、铜 800）
   - `fixture_charlie`（白金 25、金 100、银 250、铜 500）
4. 点击「入榜」，自动跳转到该玩家所在页并高亮
5. 尝试「查找玩家」或「我的排名」功能
6. 输入不存在的 ID 可看到「找不到该 PSN 用户」提示

## 真实 PSN 模式（可选）

若需拉取真实公开档案：

1. 用浏览器登录 [PlayStation](https://www.playstation.com/)
2. 同一浏览器打开 [https://ca.account.sony.com/api/v1/ssocookie](https://ca.account.sony.com/api/v1/ssocookie)，复制 JSON 里的 `npsso`
3. 在项目根目录创建 `.env`（已被 gitignore）：

```
PSN_MODE=psn
PSN_NPSSO=你的npsso值
```

4. 启动服务：`go run ./cmd/server`
5. 输入真实公开 PSN Online ID 入榜（15 分钟同步冷却）

真实模式失败态：
- 用户不存在 → 「找不到该 PSN 用户」
- 奖杯隐私 → 「该用户奖杯未公开，无法入榜」
- 未配置凭证 → 「服务端未配置 PSN 凭证」
- 限流/超时 → 「暂时无法同步奖杯，请稍后重试」
- 冷却期内 → 「同步过于频繁」

## 配置

通过环境变量或 `.env` 文件配置：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PSN_MODE` | `fixture` | `fixture` = 演示数据，`psn` = 真实档案 |
| `PSN_NPSSO` | 空 | 真实模式所需的 Sony NPSSO cookie |
| `DB_PATH` | `./data/leaderboard.db` | SQLite 数据库文件路径 |
| `LISTEN_ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |

## 拉取某个 PSN 账号的奖杯（CLI）

也可单独用 CLI 拉取奖杯汇总，不启动排行榜：

```bash
export PSN_NPSSO='你的npsso'
go run ./cmd/psnlookup cutecleverdevil
```

也可把 `PSN_NPSSO` 写进项目根目录的 `.env`（已被 gitignore）。成功时打印白金 / 金 / 银 / 铜数量和奖杯积分。

默认只打印解析后的汇总。要看索尼接口返回的**完整 JSON**（搜索用户、奖杯汇总等，不含 token）：

```bash
go run ./cmd/psnlookup --raw cutecleverdevil
```

奖杯汇总来自 `GET https://m.np.playstation.com/api/trophy/v1/users/{accountId}/trophySummary`，形态如下（字段以现场响应为准）：

```json
{
  "accountId": "0000000000000000000",
  "trophyLevel": 437,
  "trophyPoint": 200430,
  "trophyLevelBasePoint": 199890,
  "trophyLevelNextPoint": 201240,
  "progress": 40,
  "tier": 5,
  "earnedTrophies": {
    "bronze": 6212,
    "silver": 1450,
    "gold": 525,
    "platinum": 55
  }
}
```

本工具只用 `earnedTrophies`，积分按本地公式 `铜*15 + 银*30 + 金*90 + 白金*300` 计算，不采用上游 `trophyPoint`。`--raw` 不会打印 NPSSO 或 access token。

## 测试

```bash
go test ./...
```

`internal/psn/testdata/` 里是索尼文档形态的**完整 JSON**（trophySummary / universalSearch / legacy profile）。默认测试用 httptest 回放这些文件，断言 dump 与 `Summary.TrophyJSON` 和金样字节一致，不访问外网。

要对真实索尼接口抓完整 JSON：

```bash
PSN_LIVE=1 go test ./internal/psn -run TestLiveSonyCompleteJSON -v
```

需要 `PSN_NPSSO` 环境变量或根目录 `.env`。测试日志会打印完整响应，不会打印 token。

HTTP 客户端使用 `github.com/go-resty/resty/v2`。

调用顺序见 `docs/flowchart.html`（NPSSO → token → accountId → trophySummary）。

## 构建

```bash
go build ./cmd/server
./server
```

## 功能说明

### 已实现（v1）

- 中文排行榜页面，无需登录即可查看
- 演示数据模式：3 个固定假玩家，本地即可演示
- 真实 PSN 模式：拉取公开档案奖杯汇总（需 NPSSO）
- 入榜表单：提交 PSN Online ID 即可入榜或更新
- 积分公式：铜 15、银 30、金 90、白金 300（与官方一致）
- 竞赛名次：同分同名次，随后跳号（例：1、1、3）
- 分页展示：每页 50 人
- 查找玩家：按 PSN ID 跳到所在页并高亮
- 我的排名：入榜后记住 ID，快速定位
- 持久化：SQLite，进程重启后数据不丢失
- 同步冷却：真实模式 15 分钟，演示模式关闭
- 错误提示：五类失败均有对应中文说明
- 深色紧凑 UI，参考 psnprofiles 排行榜风格

### 用户故事覆盖

- US-001: 本地服务与中文空态榜页 ✅
- US-002: 奖杯积分与排名规则 ✅
- US-003: 玩家持久化与分页榜单 ✅
- US-004: 用 PSN Online ID 入榜（校验与更新）✅
- US-005: 演示数据与真实档案可切换 ✅
- US-006: 真实公开档案拉取与失败态 ✅
- US-007: 按 PSN ID 查找并跳到我的排名 ✅

## 文档

- 产品需求：`tasks/prd-psn-trophy-leaderboard.md`
- 技术方案：`tasks/spec-psn-trophy-leaderboard.md`
- 进度：`ROADMAP.md`
- 奖杯拉取流程：`docs/flowchart.html`

## 架构

```
cmd/
  server/         - HTTP 服务主程序
  psnlookup/      - CLI 拉取工具
internal/
  config/         - 环境变量配置
  http/           - HTTP 路由与模板渲染
  player/         - SQLite 持久化
  psn/            - Sony 非官方 API 客户端
  rank/           - 积分、排序、竞赛名次、分页
  trophy/         - 数据源接口（fixture / PSN）
web/
  templates/      - HTML 模板
  static/         - CSS 和占位图
```

## 依赖

- `modernc.org/sqlite` - 纯 Go SQLite 驱动（无 CGO，跨平台编译友好）
- `github.com/go-resty/resty/v2` - HTTP 客户端

## 许可

本项目用于展示排行榜产品能力，不用于生产环境。Sony 没有对个人项目开放的官方账号授权，真实档案可能拉不到或随时中断。
