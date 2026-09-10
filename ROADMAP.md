# ROADMAP

## 当前状态

v1 排行榜页面未做。已完成按 Online ID 拉取奖杯汇总的 PSN 客户端和 CLI。真实账号查询需本机 `.env` 中的 `PSN_NPSSO`（不入库）。

## 进行中

- 无

## 最近完成

- 2026-09-10 22:11 实现 `internal/psn` + `cmd/psnlookup`：NPSSO 换 token、解析 Online ID、拉 `trophySummary`
- 2026-09-10 21:43 定稿 v1 PRD（产品行为，不绑定库）：`tasks/prd-psn-trophy-leaderboard.md`
- 2026-09-10 21:43 定稿 v1 SPEC（Go / HTML / SQLite / resty / fixture 与 PSN）：`tasks/spec-psn-trophy-leaderboard.md`

## 最近验证

- 2026-09-10 22:14 真实查询 `cutecleverdevil`：白金 14、金 48、银 144、铜 636、积分 22380；与 PSNINE 公开页数字一致
- 2026-09-10 22:11 `go test ./...`：`internal/psn` 与 `cmd/psnlookup` 通过
- 2026-09-10 22:11 `.env` 已被 gitignore，不会进入提交

## 待确认

- 真实 `cutecleverdevil` 查询已通过（见「最近验证」）
