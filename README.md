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

## 文档

- 产品需求：`tasks/prd-psn-trophy-leaderboard.md`
- 技术方案：`tasks/spec-psn-trophy-leaderboard.md`
- 进度：`ROADMAP.md`
