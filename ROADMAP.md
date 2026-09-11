# ROADMAP

## 当前状态

v1 排行榜页面已完成。支持演示数据与真实 PSN 档案切换，包含完整的入榜、查找、分页、排序、持久化功能。

## 最近完成

- 2026-09-11: v1 排行榜全功能实现
  - `internal/rank`: 积分公式、竞赛名次、排序、分页
  - `internal/player`: SQLite 持久化、upsert、查询
  - `internal/trophy`: Source 接口、fixture 演示数据、PSN 真实档案包装
  - `internal/config`: 环境变量配置（PSN_MODE / PSN_NPSSO / DB_PATH / LISTEN_ADDR）
  - `internal/http`: 路由、模板、cookie、错误映射
  - `cmd/server`: 服务主程序、依赖接线
  - `web/templates` + `web/static`: 深色紧凑排行榜 UI，参考 psnprofiles 风格
  - 自动化测试：rank、player、trophy、config、http（fixture 入榜、校验、搜索、我的排名、分页）
  - 更新 README：启动命令、演示步骤、可选 PSN 凭证、架构说明
- 2026-09-11 00:02 提交 resty 完整 JSON 拉取、金样/live 测试、Lookup 注释，以及短路版 `docs/flowchart.html`
- 2026-09-10 23:13 为 PSN 客户端补充调用链注释；新增 `docs/flowchart.html` 奖杯拉取流程图
- 2026-09-10 23:04 用 resty 收拢 PSN 请求；`Summary.TrophyJSON` 保存 trophySummary 完整 body；补金样 / 失败态 / live 测试
- 2026-09-10 22:57 SPEC 第 12 节：写入索尼非官方接口现场规则（精确匹配、legacy 兜底、只用 trophySummary、不拉游戏列表）
- 2026-09-10 22:37 `psnlookup --raw`：打印索尼搜索 / trophySummary 等完整 JSON，不 dump token
- 2026-09-10 22:11 实现 `internal/psn` + `cmd/psnlookup`：NPSSO 换 token、解析 Online ID、拉 `trophySummary`
- 2026-09-10 21:43 定稿 v1 PRD（产品行为，不绑定库）：`tasks/prd-psn-trophy-leaderboard.md`
- 2026-09-10 21:43 定稿 v1 SPEC（Go / HTML / SQLite / resty / fixture 与 PSN）：`tasks/spec-psn-trophy-leaderboard.md`

## 最近验证

- 2026-09-11: `go test ./...` 通过；`go build ./cmd/server` 成功
- 2026-09-11: 演示数据模式本地启动正常，fixture_alpha / fixture_bravo / fixture_charlie 可入榜
- 2026-09-11 00:02 `go test ./...` 通过；流程图已按短路解析修订，几何校验仍有 1 条 WARNING（`POST /token` 失败箭头悬空）
- 2026-09-10 23:13 `go test ./...` 通过；`review_svg.py docs/flowchart.html` 0 ERROR / 0 WARNING
- 2026-09-10 23:04 `go test ./...` 通过；`PSN_LIVE=1` 拉到完整 `trophySummary`（含 accountId / trophyLevel / trophyPoint / earnedTrophies），本地积分与 `trophyPoint` 一致
- 2026-09-10 22:37 `go test ./...`：`--raw` dump 覆盖 trophy JSON、跳过 oauth token
- 2026-09-10 22:11 `go test ./...`：`internal/psn` 与 `cmd/psnlookup` 通过
- 2026-09-10 22:11 `.env` 已被 gitignore，不会进入提交

## v1 功能清单

### 已实现
- [x] 中文排行榜页面（US-001）
- [x] 奖杯积分公式与竞赛名次（US-002）
- [x] 玩家持久化与分页榜单（US-003）
- [x] PSN Online ID 入榜与更新（US-004）
- [x] 演示数据与真实 PSN 可切换（US-005）
- [x] 真实公开档案拉取与失败态（US-006）
- [x] 按 PSN ID 查找并跳到我的排名（US-007）
- [x] 深色紧凑 UI，参考 psnprofiles 排行榜风格
- [x] SQLite 持久化（默认 `./data/leaderboard.db`）
- [x] 真实模式 15 分钟同步冷却
- [x] Cookie 记住「我的排名」
- [x] 五类失败的中文错误提示
- [x] 演示数据 3 个固定假玩家
- [x] Online ID 校验（3–16 位，字母数字下划线连字符）
- [x] 自动化测试覆盖全部核心组件

### 未实现（Out of Scope）
- 上万用户并发压测与高并发优化
- PSN 密码登录、官方账号授权
- 用户详情页、游戏列表、近期奖杯
- 奖杯等级 1–999、完成率、稀有度
- 注册本站账号、邮箱、验证码
- 公网部署
- 管理后台、删号、封禁 UI

## 下一步（另起 PRD）

- 高并发优化：读写混合、缓存策略、限流
- 部署方案：Docker、反向代理、监控
- 扩展功能：游戏排行榜、好友榜、国家榜

