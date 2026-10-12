# QuantumNous 官方 main 合入与运行边界

任务开始于2026-10-12，固定官方提交 `dd59854050f26f4f67bb3dac5274af5e17013462`。规范日常仓库为 `.worktrees/dev`；这次高风险合并使用临时 `sync/upstream-20261012` 隔离候选。完整源码合入以该官方SHA成为最终dev祖先为必要证据，不用少量cherry-pick或旧分叉统计替代。

这份文档记录兼容契约和发布前条件。最终测试/提交/推送结果以工作区Trellis的 `official-main-preparation` 任务结果及Git为准；文档提交不表示程序已经上线。最后核验的历史生产仍是2026-10-11发布 `release/20261011-201755-snumom-resolution-billing-81580dd6` / `81580dd69327fb19d759a92348828d0c1c9d7249`，本任务没有连接生产或生成付费任务。

## 注册与模型配置

| 类型 | 本fork编号 |
| --- | --- |
| Muse/MeAI/Xs/HJ/Mimo/Lingjing | 58/59/60/61/62/63 |
| AdvancedCustom/Sub2API/NewAPI | 64/68/69 |
| kuai/sd0717/wufan | 65/66/67 |
| Anhe/ZhouSD/AutoDL H3/Diaomao/Shafu | 70/71/72/73/74 |
| Manju/Naonao/Manying/Wanchen/Yaochen/Longxia6/Snumom | 75/76/77/78/79/80/81 |
| TaskPlugin/VLLM/SGLang | 82/83/84 |
| Dummy（数量哨兵，禁止配置） | 85 |

已有渠道/能力/任务数字不重排。第三方插件或官方配置中的裸数字不能自动按本fork解释为相同供应商；导入者必须明确其来源和映射，禁止全局替换源码里的任意数字。发布预检须调查旧82渠道、能力及platform行，发现来源不明的数据时停止并核对。

native推荐目录不做名称准入，管理员公开model、映射、组、Token权限和价格继续生效。插件真实模型所有权、pinned generation和协议规则保留，不能为native开放名称而全局取消插件隔离。

## native与插件接口

旧numeric platform、native任务适配器与既有任务继续可读，通过LegacyTaskBridge适配新的TaskAdaptor。明确命名/pinned插件走官方JS路径。历史Suno/Kling/Jimeng精确URL继续native；同名官方插件仍可在通用任务/协议入口使用，不因精确兼容路由整体禁用。其它插件抢静态路径仍被拒绝。

创建结果先解析/缓冲，持久插入和结算后才输出公开ID。已经获得上游接受结果后，客户端取消不会丢弃任务：后段使用最长30秒的独立context完成持久屏障。接受后无法解析或插入失败禁止自动重试生成；插入失败退款沿既有合同处理。接受前取消没有变成后台生成授权。

## 价格、账户与权限

图片尺寸、分辨率每秒、skip_seconds与官方表达式模式共存，动态键进入原子模型+价格事务和version。native价格读公开model：专用helper初始quota0之后适配器写绝对额度，时长和组倍率只乘一次；wire与估价一致。插件USD和Token表达式USD/百万token另有单位。旧价格不自动转表达式，longxia6后缀计费继续作为独立迁移债务。

充值订单、邀请人返点credit、RebateRecord在同一订单事务内提交，提交后更新缓存/日志，重复回调不重复入账。User.M明确用于返点；密码/OAuth历史赠送差异保持，不把它误作请求收费倍率。

模型原子编辑使用 `POST /api/models/save`；读取自身待编辑模型价格使用 `GET /api/models/pricing?model=name`，二者受model_pricing.write和对应PAT scope保护，读取只含该model的Option数组。通用Options仍为RootAuth。邀请管理读取受invitation.read保护，无全部读取权限仍可看个人记录；撤销权限时旧全用户查询placeholder立即移除。

## 媒体与历史任务

需要缓存的native视频上游完成后先99，无公开结果URL/完成时间，缓存成功且稳定URL存在后100。Kuai是无缓存直链例外；图片/Suno/插件按其明确协议处理。Gemini/Vertex实时查询不抢轮询器的终态/计费所有权，已存终态不重新RPC。标准及兼容出口同时清理残留media_url/video_url/result_url。

缓存和内容代理复用供应商鉴权规则，所选Key仅用于需要鉴权的同源请求，CDN不加渠道鉴权，跨源跳转清敏感头，完整签名参数不变。Gemini不把Key附加到URI；Vertex签名结果不附加API Key，OAuth操作查询保持原协议。保留SSRF、资源/超时、Range/HEAD/206、下载重试、补偿和清理合同。

已有终态media0或缺URL行的保护只影响呈现，没有批量改数据或重放生成。若真实数据出现这类行，发布前/后须显式媒体对账；不能承诺自动完成缓存。终态CAS也不是资金/Token/SQL/Redis/独立日志的全事务保证。退款成功但quota标记失败要核对账本，不能仅据残留quota再退款。

## 数据库和回退

四钱包额度字段为bigint，启动前仍检查实际MySQL/PostgreSQL类型。不能用跳过检查代替扩列；旧type:int声明本身不能证明实际物理列为32位。Task媒体字段、Extra、Key/私有ID、旧价格快照与官方插件/表达式快照取并集，回写不丢必要JSON。

本地演练使用SQLite、MySQL8.4.11和PostgreSQL17.11：新库、分离日志/审计、二次迁移、显式32位拒绝，以及实际旧发行源码建库→原生备份→候选升级→恢复→旧源码回读。它们不等于真实生产数据验证，也不证明所有最低引擎版本/ClickHouse/OAuth供应商行为已实测。

未来发布仍需另行授权和实时预检，逐实例核对运行hash、主库/日志版本、字段/索引/重复Options、历史82/媒体异常行、Redis namespace、品牌和目录。保留各实例独立一致恢复点：冻结所有写者/支付回调/worker，等待请求/退款/batch刷库，备份主库+日志/审计+媒体+配置/插件+旧程序并明确Redis恢复/重建。

只切回旧程序是否安全必须先演练。新schema/JSON/认证期限、插件任务和新类型行可能不被旧版支持；开放写入后不能直接倒回旧SQL覆盖新交易，须保存故障窗口新增账务/外部任务/支付并对账。源码合并、发布和财务数据恢复是三个独立步骤。
