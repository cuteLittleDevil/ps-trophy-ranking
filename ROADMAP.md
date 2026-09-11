# ROADMAP

## 当前状态

v1 排行榜页面已完成。支持真实 PSN 档案拉取、`/admin/seed` 模拟灌数、入榜/刷新、查找、分页、排序与 SQLite 持久化。文档以 `tasks/spec-psn-trophy-leaderboard.md` 与 `提交历史说明.md` 为准。

## 最近完成

- 2026-09-11: 文档对齐已落地 V1（main `4bde179`）
  - 重写 `提交历史说明.md`（PR #1–#10）
  - 重写 `tasks/spec-psn-trophy-leaderboard.md` 为现行架构（无 fixture、有 seed / refresh）

- 2026-09-11: PR #10 排行榜手动刷新
  - `POST /refresh`；真实用户行显示「刷新」，`sim` 前缀不显示
  - 刷新表单带当前页，成功后不丢页码
  - 与 `/join` 共享 15 分钟 `synced_at` 冷却

- 2026-09-11: PR #9 头像
  - 删除损坏的 `placeholder.png`
  - Sony CDN `http://` 升级为 `https://`
  - 空 URL / 加载失败用 DisplayID 首字母块

- 2026-09-11: PR #8 重构架构
  - 删除 fixture 演示模式，`/join` 仅走真实 PSN
  - 新增 `/admin/seed` HTTP 管理灌数接口（追加模拟用户，压测/演示用）
  - 去掉 `PSN_MODE` 配置
  - 模拟用户 Online ID：`sim` + 7 位定宽数字（如 `sim0000123`），查重保证 UNIQUE
  - 头像占位：A–Z 随机字母，模板支持 `letter:` 前缀显示圆形字母头像
  - PRD US-005 / FR-13 改为灌数接口

- 2026-09-11: PR #7 将 chi 与 sqlite 标为 go.mod 直接依赖（Go 1.25.0）

- 2026-09-11: PR #6 HTTP 路由改用 go-chi/chi v5（`Recoverer`，方法路由）

- 2026-09-11: PR #5 顶栏布局
  - `>1500px` 单行；`769–1500px` 入榜独占第一行；`≤768px` 纵向堆叠
  - 解决 1024–1440px「查找 / 我的排名」重叠

- 2026-09-11: SPEC 对齐修复（PR #4 跟进）
  - 冷却改用 store 的 `synced_at`（store-backed cooldown），重启后仍生效
  - Score 公式统一到 `internal/rank.Score`，删除 `internal/psn/score.go`
  - Online ID 校验对齐 SPEC（允许数字开头，3–16 位，字母/数字/下划线/连字符）
  - 区分 `invalid_credentials` 与 `upstream` 错误，中文提示「PSN 凭证无效，请重新获取 NPSSO」
  - `.env.example` 补全配置项（当时含 `PSN_MODE`，PR #8 已删除）

- 2026-09-11: v1 排行榜首版（PR #4）
  - `internal/rank`: 积分公式、竞赛名次、排序、分页
  - `internal/player`: SQLite 持久化、upsert、查询
  - `internal/trophy`: Source 接口、PSN 真实档案包装（fixture 已于 PR #8 删除）
  - `internal/config`: 环境变量配置（现行：`PSN_NPSSO` / `DB_PATH` / `LISTEN_ADDR`）
  - `internal/http`: 路由、模板、cookie、错误映射
  - `cmd/server`: 服务主程序、依赖接线
  - `web/templates` + `web/static`: 深色紧凑排行榜 UI，参考 psnprofiles 风格
  - 自动化测试：rank、player、trophy、config、http（校验、搜索、我的排名、分页、seed、refresh）
  - 更新 README：启动命令、seed 演示、可选 PSN 凭证、架构说明
- 2026-09-11 00:02 提交 resty 完整 JSON 拉取、金样/live 测试、Lookup 注释，以及短路版 `docs/flowchart.html`
- 2026-09-10 23:13 为 PSN 客户端补充调用链注释；新增 `docs/flowchart.html` 奖杯拉取流程图
- 2026-09-10 23:04 用 resty 收拢 PSN 请求；`Summary.TrophyJSON` 保存 trophySummary 完整 body；补金样 / 失败态 / live 测试
- 2026-09-10 22:57 SPEC 第 12 节：写入索尼非官方接口现场规则（精确匹配、legacy 兜底、只用 trophySummary、不拉游戏列表）
- 2026-09-10 22:37 `psnlookup --raw`：打印索尼搜索 / trophySummary 等完整 JSON，不 dump token
- 2026-09-10 22:11 实现 `internal/psn` + `cmd/psnlookup`：NPSSO 换 token、解析 Online ID、拉 `trophySummary`
- 2026-09-10 21:43 定稿 v1 PRD（产品行为，不绑定库）：`tasks/prd-psn-trophy-leaderboard.md`
- 2026-09-10 21:43 定稿 v1 SPEC 初稿（后经 PR #6/#8/#10 改为现行架构）：`tasks/spec-psn-trophy-leaderboard.md`

## 最近验证

- 2026-09-11: `go test ./...` 通过；`go build ./cmd/server` 成功
- 2026-09-11: 无 NPSSO 时用 `POST /admin/seed` 灌模拟用户即可看榜；`/join` 返回「服务端未配置 PSN 凭证」
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
- [x] `/admin/seed` 灌入模拟数据（US-005）
- [x] 真实公开档案拉取与失败态（US-006）
- [x] 按 PSN ID 查找并跳到我的排名（US-007）
- [x] 深色紧凑 UI，参考 psnprofiles 排行榜风格
- [x] SQLite 持久化（默认 `./data/leaderboard.db`）
- [x] PSN 入榜 15 分钟同步冷却
- [x] Cookie 记住「我的排名」
- [x] 失败态中文错误提示（含未配置凭证 / 凭证无效 / 冷却 / 刷新未入榜）
- [x] 排行榜每行手动刷新（`POST /refresh`，模拟用户不显示按钮）
- [x] 头像：Sony CDN http 升 https；加载失败首字母回退
- [x] 模拟用户追加灌数（随机奖杯、字母头像）
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

