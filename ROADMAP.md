# ROADMAP

## 当前状态

v1 排行榜页面未做。已完成按 Online ID 拉取奖杯汇总的 PSN 客户端和 CLI。真实账号查询需本机 `.env` 中的 `PSN_NPSSO`（不入库）。

## 进行中

- 无

## 最近完成

- 2026-09-11 00:02 提交 resty 完整 JSON 拉取、金样/live 测试、Lookup 注释，以及短路版 `docs/flowchart.html`
- 2026-09-10 23:13 为 PSN 客户端补充调用链注释；新增 `docs/flowchart.html` 奖杯拉取流程图
- 2026-09-10 23:04 用 resty 收拢 PSN 请求；`Summary.TrophyJSON` 保存 trophySummary 完整 body；补金样 / 失败态 / live 测试
- 2026-09-10 22:57 SPEC 第 12 节：写入索尼非官方接口现场规则（精确匹配、legacy 兜底、只用 trophySummary、不拉游戏列表）
- 2026-09-10 22:37 `psnlookup --raw`：打印索尼搜索 / trophySummary 等完整 JSON，不 dump token
- 2026-09-10 22:11 实现 `internal/psn` + `cmd/psnlookup`：NPSSO 换 token、解析 Online ID、拉 `trophySummary`
- 2026-09-10 21:43 定稿 v1 PRD（产品行为，不绑定库）：`tasks/prd-psn-trophy-leaderboard.md`
- 2026-09-10 21:43 定稿 v1 SPEC（Go / HTML / SQLite / resty / fixture 与 PSN）：`tasks/spec-psn-trophy-leaderboard.md`

## 最近验证

- 2026-09-11 00:02 `go test ./...` 通过；流程图已按短路解析修订，几何校验仍有 1 条 WARNING（`POST /token` 失败箭头悬空）
- 2026-09-10 23:13 `go test ./...` 通过；`review_svg.py docs/flowchart.html` 0 ERROR / 0 WARNING
- 2026-09-10 23:04 `go test ./...` 通过；`PSN_LIVE=1` 拉到完整 `trophySummary`（含 accountId / trophyLevel / trophyPoint / earnedTrophies），本地积分与 `trophyPoint` 一致
- 2026-09-10 22:37 `go test ./...`：`--raw` dump 覆盖 trophy JSON、跳过 oauth token
- 2026-09-10 22:11 `go test ./...`：`internal/psn` 与 `cmd/psnlookup` 通过
- 2026-09-10 22:11 `.env` 已被 gitignore，不会进入提交

## 待确认

- 真实 `cutecleverdevil` 查询已通过（见「最近验证」）
