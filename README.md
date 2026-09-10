# ps-trophy-ranking

PSN 奖杯用户排行榜。排行榜页面尚未实现；当前可在配置 NPSSO 后按 Online ID 拉取奖杯汇总。

## 拉取某个 PSN 账号的奖杯

1. 用浏览器登录 [PlayStation](https://www.playstation.com/)
2. 同一浏览器打开 [https://ca.account.sony.com/api/v1/ssocookie](https://ca.account.sony.com/api/v1/ssocookie) ，复制 JSON 里的 `npsso`（等同密码，不要提交进仓库）
3. 在项目根目录：

```bash
export PSN_NPSSO='你的npsso'
go run ./cmd/psnlookup cutecleverdevil
```

也可把 `PSN_NPSSO` 写进项目根目录的 `.env`（已被 gitignore）。成功时打印白金 / 金 / 银 / 铜数量和奖杯积分。

## 文档

- 产品需求：`tasks/prd-psn-trophy-leaderboard.md`
- 技术方案：`tasks/spec-psn-trophy-leaderboard.md`
- 进度：`ROADMAP.md`
