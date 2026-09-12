# PSN 奖杯排行榜 — API 与双 1w 验收（摘要）

> 完整技术方案见 [`tasks/spec-psn-trophy-leaderboard.md`](../tasks/spec-psn-trophy-leaderboard.md)。本文只摘录 **只读 JSON 榜单 API** 与 **双 1w 验收**，便于查阅。

## GET `/api/leaderboard`

只读、无鉴权。Query：`page`（默认 1）、`page_size`（默认 50，合理钳制）。复用 `memrank.Leaderboard.Page` / total 辅助（Top1000 vs 全量 ranked）。

```json
{
  "page": 1,
  "page_size": 50,
  "total": 100000,
  "total_pages": 2000,
  "items": [
    {"rank": 1, "online_id": "...", "avatar_url": "...", "bronze": 0, "silver": 0, "gold": 0, "platinum": 0, "score": 0}
  ]
}
```

不改变 Left-Right / WAL 设计；SSR HTML `/` 不要求 10k QPS。

## 双 1w 验收（已批准）

1. **写**：冷路径 `/admin/seed`，可见玩家增长 ≥ 10k/s（短突发 OK）
2. **读**：JSON page API 合计 ≥ 10k QPS，失败约 0；可与写入并发
3. **读混合**：50% Top1000 热区 `page∈[1,20]`（`page_size=50`），50% 全库随机页
4. **SSR**：HTML `/` **不**要求 10k QPS

脚本：`scripts/stress_dual_1w.sh`
