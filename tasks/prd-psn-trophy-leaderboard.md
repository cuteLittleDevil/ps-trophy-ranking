# PRD: PSN 奖杯排行榜（v1）

实现方案见 `/Users/chendon/GolandProjects/tmp/ps-trophy-ranking/tasks/spec-psn-trophy-leaderboard.md` 。本文件只描述产品行为，不指定语言、框架、库或存储引擎。

## 1. Introduction / Overview

做一个本地可运行的 PSN 奖杯用户排行榜页面，供岗位简历展示「排行榜」产品能力。访问者打开页面即可看到全局榜；要入榜时填写公开的 PSN Online ID（不收集 PS5 密码），系统拉取该档案的奖杯汇总，按官方奖杯积分写入排行榜，并可搜索 / 跳到该 ID 的名次。

参考形态：[psnprofiles.com/leaderboard](https://psnprofiles.com/leaderboard) 的全局榜，而不是其用户详情、游戏榜或整站功能。

v1 明确不做账号密码登录，也不做上万并发。Sony 没有对个人项目开放的官方账号授权，真实档案可能拉不到或随时中断，因此必须同时提供 **不依赖外部网络的演示数据**，保证本地演示能跑通。

## 2. Goals

- 未登录即可查看中文全局排行榜（名次、头像、PSN ID、铜/银/金/白金数量、奖杯积分）
- 提交公开 PSN Online ID 即可入榜或更新已有记录，不创建本站账号、不收密码
- 积分公式与官方一致：铜 15、银 30、金 90、白金 300（[PlayStation Wiki](https://playstation.fandom.com/wiki/Trophies)、[PSNProfiles 说明](https://psnprofiles.com/guide/18274-the-trophy-system-explained)、[2020 年白金改为 300 分](https://www.gematsu.com/2020/10/playstation-trophy-system-updates-announced-new-levels-calculation-structure-and-level-icons)）
- 积分相同按白金 → 金 → 银 → 铜 → Online ID 字母序折
- 支持分页，以及按 PSN ID 查找 / 跳到「我的排名」
- 同一 Online ID 重复提交是更新，不是重复行
- 本地启动后用浏览器即可演示；演示数据模式下不依赖 Sony 网络
- 真实拉取失败时给出可理解的中文错误（不存在、隐私、限流、未配置凭证、网络错误）

## 3. User Stories

### US-001: 本地服务与中文空态榜页
**Description:** As a 面试官 / 开发者, I want 一条命令启动服务并打开中文排行榜页 so that 没有数据时也能看到产品骨架。

**Acceptance Criteria:**
- [ ] 按 README 的启动方式在本地打开排行榜页，默认访问地址写在 README
- [ ] 打开首页看到中文标题、入榜表单、排行榜表格表头
- [ ] 榜上无人时显示明确空态文案（例如「还没有玩家入榜」），不显示空白坏表
- [ ] 项目可构建
- [ ] Verify in a browser

### US-002: 奖杯积分与排名规则
**Description:** As a 系统, I want 用可测试的规则计算积分和排序 so that 排行结果可复现、可讲清。

**Acceptance Criteria:**
- [ ] `score = bronze*15 + silver*30 + gold*90 + platinum*300`
- [ ] 排序键依次为：score 降序、platinum 降序、gold 降序、silver 降序、bronze 降序、online_id 升序
- [ ] 名次为竞赛名次：同分同名次，下一名次跳过（例：1、1、3）
- [ ] 自动化测试覆盖：普通分差、完全同分、仅白金不同、空奖杯全 0
- [ ] 相关自动化测试通过

### US-003: 玩家持久化与分页榜单
**Description:** As a 访客, I want 看到按名次排列、可翻页的全局榜 so that 人多时仍能浏览。

**Acceptance Criteria:**
- [ ] 玩家数据重启进程后仍在（不依赖进程内存）
- [ ] 每页 50 人；第 2 页第一条的全局名次接续，而不是从 1 重计
- [ ] 每一行展示：名次、头像（无头像用占位图）、PSN Online ID、白金/金/银/铜数量、奖杯积分
- [ ] 页码超出范围时显示空列表和「没有更多玩家」，不出现服务错误页
- [ ] 相关自动化测试通过
- [ ] Verify in a browser

### US-004: 用 PSN Online ID 入榜（校验与更新）
**Description:** As a 玩家, I want 填写公开的 PSN Online ID 加入或更新排行榜 so that 我不需要把 PS5 密码交给这个站点。

**Acceptance Criteria:**
- [ ] 首页有入榜表单，字段为 PSN Online ID
- [ ] Online ID 校验：去首尾空格后长度 3–16，只允许字母、数字、`-`、`_`；非法时表单下显示中文错误，不向外同步奖杯
- [ ] 空提交显示「请输入 PSN Online ID」，不向外同步奖杯
- [ ] 同一 Online ID 再次入榜：更新奖杯数字和积分，不新增行；保留首次入榜时间，更新「最近同步时间」
- [ ] 入榜成功后跳到该玩家所在页，并高亮其行
- [ ] 相关自动化测试通过
- [ ] Verify in a browser

### US-005: 演示数据与真实档案可切换
**Description:** As a 开发者, I want 用同一套入榜流程切换演示数据 / 真实 PSN so that 没凭证时也能完整演示入榜。

**Acceptance Criteria:**
- [ ] 可通过配置在演示数据与真实 PSN 之间切换，默认使用演示数据
- [ ] 演示数据至少内置 3 个不同积分的假玩家；提交这些 ID 能入榜且数字固定可断言
- [ ] 演示数据对未知 ID 返回「找不到该 PSN 用户」
- [ ] 入榜只需要奖杯汇总：online_id、avatar_url、platinum、gold、silver、bronze；v1 不展示游戏列表
- [ ] 演示数据路径的自动化测试不访问外网
- [ ] Verify in a browser（演示数据入榜）

### US-006: 真实公开档案拉取与失败态
**Description:** As a 玩家, I want 在配置了服务端凭证后用真实公开档案入榜，并在失败时看到原因 so that 不是一个只能播假数据的玩具。

**Acceptance Criteria:**
- [ ] 真实模式下，系统按 Online ID 拉公开奖杯汇总；凭证只存在于运行配置，不进仓库
- [ ] 用户不存在：页面显示「找不到该 PSN 用户」
- [ ] 档案隐私不允许查看奖杯：显示「该用户奖杯未公开，无法入榜」
- [ ] 未配置凭证：显示「服务端未配置 PSN 凭证」，不出现服务错误页
- [ ] 上游限流或网络错误：显示「暂时无法同步奖杯，请稍后重试」
- [ ] 同一 Online ID 同步冷却 15 分钟（演示数据模式可关闭或缩短）；冷却期内提示「同步过于频繁」并保留旧数据
- [ ] 不把凭证、会话令牌写入日志或页面
- [ ] 自动化测试覆盖上述失败路径，且不依赖真实外网
- [ ] Verify in a browser（至少验证演示数据失败态；真实 PSN 取决于是否配置凭证）

### US-007: 按 PSN ID 查找并跳到我的排名
**Description:** As a 访客, I want 输入 PSN ID 定位到该行 so that 人多时不用翻页找自己。

**Acceptance Criteria:**
- [ ] 榜页有查找框，提交后若在榜中：跳到所在页并高亮该行
- [ ] 若 ID 合法但不在榜中：显示「该玩家尚未入榜」，不出现找不到页面的白屏
- [ ] 入榜成功后记住该 Online ID（刷新页面后仍可用）；点击「我的排名」等价于查找该 ID
- [ ] 没有记住的 ID 时，「我的排名」提示「先入榜或先查找」
- [ ] 查找非法 ID 时复用 US-004 的校验文案
- [ ] 相关自动化测试通过
- [ ] Verify in a browser

## 4. Functional Requirements

- FR-1: 系统必须提供无需登录即可访问的中文排行榜首页。
- FR-2: 系统必须在首页展示入榜表单，唯一输入为 PSN Online ID。
- FR-3: 系统不得收集或存储 PS5 / PSN 密码。
- FR-4: 系统必须按 `bronze*15 + silver*30 + gold*90 + platinum*300` 计算奖杯积分。
- FR-5: 系统必须按积分、白金、金、银、铜、Online ID 的顺序排序。
- FR-6: 系统必须使用竞赛名次（同分同名次，随后跳号）。
- FR-7: 系统必须分页展示排行榜，每页 50 条。
- FR-8: 系统必须在每一行展示名次、头像、Online ID、白金/金/银/铜数量、积分。
- FR-9: 系统必须在无人入榜时展示空态，而不是错误页。
- FR-10: 系统必须拒绝非法 Online ID，并在表单旁显示中文错误。
- FR-11: 系统必须把同一 Online ID 的再次提交视为更新而非插入。
- FR-12: 系统必须在入榜成功后跳转到该玩家所在页并高亮该行。
- FR-13: 系统必须提供演示数据与真实 PSN 两种数据源，并由配置切换，默认演示数据。
- FR-14: 系统必须在真实模式下使用服务端凭证拉取公开档案；凭证不得写入仓库、页面或常规日志。
- FR-15: 系统必须把「用户不存在 / 未公开 / 未配置凭证 / 限流或网络错误 / 同步过频」显示为互不相同的中文说明。
- FR-16: 系统必须对同一 Online ID 的真实同步施加 15 分钟冷却。
- FR-17: 系统必须支持按 Online ID 查找已入榜玩家。
- FR-18: 系统必须在查找未入榜但格式合法的 ID 时说明尚未入榜。
- FR-19: 系统必须在成功入榜后记住该 Online ID，并提供「我的排名」入口。
- FR-20: 系统必须在进程重启后仍能读出已入榜数据。
- FR-21: 系统必须在 README 写出启动命令、演示数据步骤、以及（可选）配置真实 PSN 凭证的方式。

## 5. Non-Goals (Out of Scope)

- 上万用户并发压测与高并发优化 -- 另起 PRD（已倾向读写混合）
- PSN 密码登录、官方账号授权、让终端用户把会话令牌粘贴到浏览器
- 用户详情页、游戏列表、近期奖杯、游戏排行榜、好友榜、国家榜
- 奖杯等级 1–999、完成率、稀有度
- 注册本站账号、邮箱、验证码、权限角色
- 公网部署、生产环境
- 爬取 [psnprofiles.com](https://psnprofiles.com/leaderboard) 或使用其页面 HTML 作为数据源
- 实时在线状态、推送、评论、点赞
- 管理后台、删号、封禁 UI（开发者可清本地数据除外）

## 6. Design Considerations

- 单页为主：顶部简介 + 入榜/查找 + 表格。
- 视觉参考 psnprofiles 全局榜：深色背景、紧凑表格、白金/金/银/铜用颜色区分。不复制其 Logo、样式文件或图片资源。
- 文案中文；Online ID、trophy 专有词保持英文或通用缩写（PSN、ID）。
- 高亮行需在无颜色辨识时仍能看出（左边框或「你」标记），不只靠颜色。
- 桌面宽度为演示主场景；窄屏表格可横向滚动，不在 v1 做独立移动端布局。

## 7. Success Metrics

- 未配置 PSN 凭证时，按 README 用演示数据能完成：看空榜 → 入榜 3 人 → 翻页（若不足 50 人则验证只有 1 页）→ 查找命中高亮 → 查找未入榜 ID
- 积分与名次和 US-002 测试用例完全一致
- 非法 ID、空提交、冷却、隐私/不存在等失败不出现空白错误页
- 凭证与会话令牌不出现在仓库、页面和常规日志中
- 从克隆到浏览器看到榜，步骤不超过 README 中的 3 条命令级操作

## 8. Open Questions

- 头像无法显示时，是否统一用本地占位图即可？（产品默认：可以）
- Online ID 展示是否采用数据源返回的规范大小写？（产品默认：是）
- 竞赛名次在简历讲解里是否改为稠密名次（1、1、2）？当前按竞赛名次实现。
