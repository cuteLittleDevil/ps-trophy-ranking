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

## 文档

- 产品需求：`tasks/prd-psn-trophy-leaderboard.md`
- 技术方案：`tasks/spec-psn-trophy-leaderboard.md`
- 进度：`ROADMAP.md`
- 奖杯拉取流程：`docs/flowchart.html`
