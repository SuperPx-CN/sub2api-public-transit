# ai-transit.v1 只读服务

独立 Go 服务，仅从现有 Sub2API PostgreSQL 数据库读取公开资料，提供：

- GET /.well-known/ai-transit.json
- GET /api/public/transit/v1/snapshot

输出保持 schema_version: ai-transit.v1，system: sub2api 表示数据来源。没有首页、前端、登录、管理、推理网关、支付执行、主动探测、定时聚合或数据库初始化功能。不使用 Redis。

## 本地运行

需要 Go 1.26.5 和已授权的 PostgreSQL 只读账号。先阅读 [部署及迁移指南](deploy/README.md) 中的授权和结构兼容说明。

~~~sh
export PUBLIC_BASE_URL=https://transit.example.com
export DATABASE_HOST=postgres.example.internal
export DATABASE_PORT=5432
export DATABASE_DBNAME=sub2api
export DATABASE_USER=transit_reader
export DATABASE_PASSWORD='your-reader-password'
export DATABASE_SSLMODE=require
make build
./build/transit
~~~

~~~sh
curl --fail https://transit.example.com/.well-known/ai-transit.json
curl --fail https://transit.example.com/api/public/transit/v1/snapshot
~~~

PUBLIC_BASE_URL 必填，必须为 HTTP(S) origin，不含用户名、路径前缀、查询或 fragment。绝对地址不使用 Host、Forwarded 或 X-Forwarded-*。原站页面地址可以分别通过 STATION_HOME_URL、STATION_PRICE_URL、STATION_MONITOR_URL 指定；未配置时不生成页面地址。发现文档不再指向本服务页面，快照不再输出 public_page_url。

## 行为和兼容范围

- 未知路径返回 404；两个接口只支持 GET，其余方法（包括 HEAD）返回 405 和 Allow: GET。无 SPA fallback、旧别名或 /health。
- settings.public_transit_enabled 缺省开启；false、0、off、disabled（忽略大小写和首尾空格）关闭，两个接口返回 404。
- 数据库不可用、核心结构不兼容、权限不足或查询失败返回 503，响应仅包含 service unavailable，不泄露 SQL、连接串或凭据。
- 使用数据库连接 default_transaction_read_only=on、只读事务和 SELECT 白名单；快照使用 repeatable-read。没有迁移器、DDL/DML、后台刷新或 Redis 客户端。
- 进程内快照缓存 60 秒，由请求触发刷新；每次请求仍核验开关、数据库连通性和核心读取权限。成功的聚合快照在 TTL 内可保持旧值；失败不缓存、不延长旧数据有效期。
- 渠道 mapping 与 pricing 并集、大小写去重、区间价格、图片尺寸价、分组自定义目录保持原语义。分组倍率单独输出，不再次乘入模型价格。价格单位为 USD/token 或 USD/request，不能当作每百万 token。
- 标准定价使用随二进制嵌入的本地资料和提取的原有别名/回退规则；可用 PRICING_FILE 指定只读 JSON 文件。启动时加载一次，不联网、不下载、不定时更新。资料来源和版本随代码版本追踪。
- 缓存命中率与监控 v1 可用率沿用百分比（0–100）；监控 v2 success/request 沿用比例（0–1），不擅自改变既有字段单位。
- 监控开关、模式和 v2 平台/模型/分组配置继续生效。v1 读取已存历史；v2 读取已有 12 小时 rollup、错误分类、直方图和覆盖水位，沿用忽略错误和健康评分规则。没有样本时返回空 monitoring；所需可选监控表缺失时通过 completeness.warnings 说明。已有表缺列或权限不足属于读取故障，返回 503。
- 兼容基线为本仓库精简前 Sub2API 结构，不能保证任意上游版本兼容；结构差异必须由运维确认，不自动修改源数据库。

## 配置

| 环境变量 | 默认值 / 用途 |
| --- | --- |
| PUBLIC_BASE_URL | 必填，公开服务 origin |
| SERVER_HOST / SERVER_PORT | 0.0.0.0 / 8080 |
| DATABASE_HOST / DATABASE_PORT | localhost / 5432 |
| DATABASE_DBNAME / DATABASE_USER | sub2api / transit_reader |
| DATABASE_PASSWORD | 数据库密码 |
| DATABASE_SSLMODE | disable；生产建议 verify-full，部署示例使用 require |
| DATABASE_SSLROOTCERT | 可选，只读 CA 文件路径 |
| STATION_HOME_URL / STATION_PRICE_URL / STATION_MONITOR_URL | 可选，原站绝对页面 URL |
| PRICING_FILE | 可选，本地 LiteLLM 格式 JSON；默认使用嵌入资料 |

原 Redis、AUTO_SETUP、管理员、JWT、TOTP 变量即使残留，也不会被读取。

## 构建与验证

~~~sh
make check                         # 单测、竞态检测、go vet、Helm lint/render
sh deploy/tests/source-check.sh    # 无旧网关、Redis、定时器和写 SQL
sh deploy/tests/isolation-test.sh   # 临时 PostgreSQL、只读角色、镜像构建和容器冒烟
~~~

集成脚本只创建并删除本机临时容器，不使用现网连接。测试 fixture 位于 backend/internal/transit/testdata/schema.sql，不是生产迁移。只有显式设置 TEST_DATABASE_URL 才会运行数据库测试，数据库名称必须为 transit_test，且必须为空的一次性测试库。不要指向共享数据库。固定 JSON 回归样本覆盖发现文档、v1/v2 快照、定价、目录过滤、缓存统计及公开字段。

镜像继续通过现有 vX.Y.Z 标签 / 手动重建流程发布到当前仓库 GHCR，保留版本、commit 和稳定版 latest 标签。没有 Chart 发布流程。

## 许可证与来源

保留 [LICENSE](LICENSE) 的原始条款。公开协议、定价和监控转换派生自 Wei-Shaw/sub2api 及本仓库已有实现；本项目是去除写入业务能力后的独立服务，并非完整 Sub2API。详见 [NOTICE](NOTICE)。
