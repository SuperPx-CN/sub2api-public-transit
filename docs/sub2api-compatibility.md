# Sub2API 模型配置兼容性

本项目修复版本为 **0.2.1**，兼容起点为上游 **Sub2API v0.2.8**。两个项目使用独立版本线，部署本服务时镜像与 Chart 使用本项目版本号。复核日期：2026-09-29。

## 已核实的官方来源

- [Sub2API v0.2.8](https://github.com/Wei-Shaw/sub2api/tree/v0.2.8)，提交 `fd80b08c90b55edcad5b00171b53f08721d30da1`，已包含 `model_allowlist` 及迁移 235、236。
- [提交 cff3f89850738e3504e35b01c9b4028d62b65a01](https://github.com/Wei-Shaw/sub2api/commit/cff3f89850738e3504e35b01c9b4028d62b65a01)，日期 2026-09-05，将模型目录配置升级为模型准入白名单。
- [历史迁移 143](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/migrations/143_group_models_list_config.sql) 已定义 `models_list_config`；[迁移 235](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/migrations/235_group_model_allowlist.sql) 将其更名；[迁移 236](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/migrations/236_group_model_allowlist_repair.sql) 处理旧列、两列并存和两列均缺失等恢复场景。

本项目 0.2.0 的查询仍强制读取旧列，是适配缺陷。仅凭“旧列不存在”不能认定源数据库漏执行迁移。本次没有检查或修改现网数据库，以下结构差异均在一次性 PostgreSQL 测试库验证。

## 结构选择与错误处理

| groups 配置列 | 选用字段 | 目录行为 |
| --- | --- | --- |
| 仅 `model_allowlist` | 新列 | 关闭时输出渠道/定价目录；开启时筛选该目录 |
| 仅 `models_list_config` | 旧列 | 保留原有目录补充行为与 JSON 回归样本 |
| 两列并存 | 始终使用新列 | 新列关闭、为空对象或 NULL 时也不回退旧列 |
| 两列均不存在 | 无 | 保留渠道/定价目录，并在 `completeness.warnings` 说明分组模型配置缺失 |

预检和快照共用字段探测与配置解析代码。在只读事务内通过 `pg_catalog.pg_attribute` 和 `'groups'::regclass` 查找字段，跟随应用的 `search_path`，排除已删除列。无需授权不存在或未选中的配置列；存在但不可读的字段不会被误判为缺失。动态 SELECT 仅使用两个硬编码列名，仅读取 `id` 和选定配置列，不读取整行。

选定列必须是 JSON/JSONB，配置结构为 `{"enabled": boolean, "models": string[]}`；缺省字段、SQL NULL 或 JSON null 按关闭/空配置处理。列级权限不足、字段类型错误、公开分组配置无法解析，以及核心表/列缺失，均通过两个公开接口返回脱敏的 `503 service unavailable`，不回退、不伪装为无配置。配置检查覆盖 discovery 和快照缓存命中请求。

探测结果不永久缓存。每次请求重新探测；快照缓存沿用原有 60 秒 TTL，字段更名或配置修改后，成功缓存可在 TTL 内保留旧值，到期后使用新结构刷新。读取错误不延长旧缓存有效期。两个接口路径、`ai-transit.v1` 字段和价格/百分比单位保持不变。

## 白名单匹配边界

新列启用时，对渠道 mapping 与 pricing 并集中的具体模型，按**客户端可见名称**调用上游 `Allows` 的纯匹配逻辑：

- 精确名称与末尾 `*` 前缀匹配；条目去首尾空格，比较大小写不敏感。
- 候选归一化沿用 v0.2.8：Gemini `models/` 前缀、`-thinking` 宽容规则及 Claude OAuth 日期别名、已知 OpenAI 推理后缀与模型别名。归一化的方向和大小写边界也沿用上游，不自行扩展别名集合。
- 渠道将 `Alias` 映射到 `Text` 时，白名单允许 `Text` 不会自动允许 `Alias`。允许 `Alias` 时保留原先按 `Text` 取得的价格、区间及来源。
- 白名单仅过滤现有具体候选，不调用上游账号目录、网关或默认模型发现服务；不把通配字符串、未确认支持的精确条目或本地价格库中的其他模型添加到目录。
- 开启但列表为空时返回 `models: []`。不使用旧字段的“enabled 且非空”判断将空白名单误当成关闭。

本地 `group_model_allowlist.go` 和 `group_model_allowlist_aliases.go` 的纯函数来源：

- [service/group_model_allowlist.go](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/internal/service/group_model_allowlist.go) 的 `Allows` 与候选生成。
- [pkg/claude/constants.go](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/internal/pkg/claude/constants.go) 的模型 ID 映射。
- [service/openai_compat_model.go](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/internal/service/openai_compat_model.go)、[service/openai_model_alias.go](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/internal/service/openai_model_alias.go)、[service/openai_codex_transform.go](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/internal/service/openai_codex_transform.go) 以及 [pkg/openai/constants.go](https://github.com/Wei-Shaw/sub2api/blob/v0.2.8/backend/internal/pkg/openai/constants.go) 的纯名称归一化函数和常量；复用本仓库已有的等价纯定价辅助函数。

这是从 v0.2.8 开始的字段结构兼容和已核实匹配语义，不是对任意未来上游版本所有数据库结构的兼容承诺。原只读数据库、监控可选表、无账号读取、无 Redis 和无在线定价下载的边界继续生效。

## 验证与部署

`make check` 覆盖 Go 单测、竞态检测、go vet 和 Helm lint/render。`sh deploy/tests/isolation-test.sh` 在本机一次性 PostgreSQL 中验证四种结构的两接口响应、列级只读授权、选定列错误、字段运行中更名与缓存到期、search_path、核心表缺失、断连、错误脱敏与 Redis 零连接，然后构建镜像并执行容器冒烟。旧结构的 discovery、v1/v2 JSON 样本保持不变。

基础列和可选配置列的授权示例见 [部署说明](../deploy/README.md)。本次只准备仓库与 0.2.1 部署示例，不发布镜像、不打发布标签、不部署现网、不对共享源库执行 DDL/DML。测试 fixture 不可作为生产迁移。
