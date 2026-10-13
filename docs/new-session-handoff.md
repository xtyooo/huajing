# 新对话接手：画境 / New API 二开

整理日期：2026-10-12，2026-10-13补充真实官方主线实施记录，用户时区 Asia/Shanghai。此文件是接手索引，不是启动部署的授权。

## 1. 先读这些文件

1. 当前工作树的 `AGENTS.md`。
2. [完整主题历史](./conversation-history-2026-10-12.md)。
3. [开发踩坑与 AI 自我复盘](./development-pitfalls.md)。
4. [渠道长期约定](./channel-integration-policy.md) 与 [模型准入检查](./channel-model-admission-audit.md)。
5. [snumom 请求与计费](./snumom-video.md)。

本地工作区根目录另有 `.trellis/workflow.md`、`.trellis/spec/`、`.trellis/tasks/` 和 `.trellis/workspace/xtyooo/journal-1.md`。
它们不在代码仓库内；不能在 `.worktrees/dev/.trellis` 查找，也不能把工作区根目录当成 Git 仓库。
不能依靠新聊天自动获得旧聊天全文或记忆已经被自动索引；以上文件是可靠的接手入口。

## 2. 路径、分支与状态边界

| 项目 | 已核对的值 |
| --- | --- |
| 工作区根目录 | `D:/Code/ai_workspace/兼职/newapi二开` |
| 规范日常 Git checkout | `D:/Code/ai_workspace/兼职/newapi二开/.worktrees/dev` |
| 长期分支 | `dev`，`origin/HEAD` 指向 `origin/dev` |
| origin | `https://github.com/xtyooo/huajing.git` |
| 当前配置的官方 upstream | `https://github.com/QuantumNous/new-api.git` |
| 本轮文档整理前的功能基线 | `81580dd69327fb19d759a92348828d0c1c9d7249` |
| 上次生产功能提交 | `fix: support snumom resolution per-second billing` |

上述功能基线是2026-10-11历史生产。2026-10-12接手文档HEAD为 `dfa50e9716df5141194c62fc4a8103daaf577b3e`；后续明确授权的官方合入另见第6节及Git，不会部署新程序。
新窗口须重新执行 Git 状态检查，不能把旧功能/文档HEAD当作永远最新的dev，也不能把未发布的官方合入当生产版本。

```powershell
$project = 'D:\Code\ai_workspace\兼职\newapi二开'
$repo = Join-Path $project '.worktrees\dev'
Set-Location -LiteralPath $repo
git status --short
git branch --show-current
```

只有确认改动归属、当前工作树安全且分支为 dev 后，再 `git fetch origin`、`git pull --ff-only origin dev`。
发现不相关改动应保留，不能 reset/clean/checkout 覆盖、混入本次提交或擅自清理旧工作树。
日常任务直接 dev；只有高风险真实官方合入才允许临时 `sync/upstream-YYYYMMDD` 分支/隔离工作树。

## 3. 生产：最后核验的快照，不代表今天实时状态

最后实际发布及健康验证发生在 **2026-10-11**。本次2026-10-12文档任务没有连接服务器验证或改变线上服务。

| 项目 | 快照 |
| --- | --- |
| 新服务器 | `152.53.240.140` |
| 旧服务器 | `140.235.37.98`，历史部署目标，已被用户替换，不得误连 |
| 功能提交 | `81580dd69327fb19d759a92348828d0c1c9d7249` |
| 发布编号 | `20261011-201755-snumom-resolution-billing-81580dd6` |
| 已推送发布标签 | `release/20261011-201755-snumom-resolution-billing-81580dd6` |
| Linux main SHA256 | `2bc0ce7019ff9d8c61829fcfbaa58c94bda0b1d263546fff9e444fa2a8070b29` |
| main.gz SHA256 | `38499c95b35ef4aeacaa9db2e8ba139cc992af3b20ddfeb6209b95cf65aa6fee` |

| 实例 | 服务 / 端口 | 当前发布根目录 | 环境约定 | 公网状态接口 |
| --- | --- | --- | --- | --- |
| 画境 | `new-api.service` / 3000 | `/apps/releases/` | 各release的 `.env` 从该实例旧release复制 | `https://huajingapi.top/api/status` |
| 小太阳 | `new-api-xty.service` / 3001 | `/srv/xty-new-api/releases/` | 固定 `/srv/xty-new-api/config/app.env` | `https://art.xtyooo.top/api/status` |

在各发布根目录加发布编号即可得到此次工作目录；两个运行文件均为其目录下的 `main`。
核验时 active/running、NRestarts=0、内外HTTP200、程序hash一致、数据库/Redis/媒体/品牌隔离正常，日志没有新增fatal/media-cache失败。
画境当时 `system_name` 实际值是“画镜”，小太阳是“小太阳”；发布需捕获并保持各自原值，不得硬编码改成自己认为正确的名字。

备份/日志/精确回滚命令：

```text
/apps/backups/20261011-201755-snumom-resolution-billing-81580dd6/RELEASE_RECORD.txt
/srv/xty-new-api/backups/20261011-201755-snumom-resolution-billing-81580dd6/RELEASE_RECORD.txt
```

前一版本为 `20261011-190023-snumom-dreamina-grok-4c01ab82`。
不要把二进制回滚当作数据库迁移回滚，执行时必须使用目标实例自己的 RELEASE_RECORD。
本地详细证据在工作区 `.trellis/tasks/archive/2026-10/10-11-snumom-resolution-billing/results.md`。
SSH 密钥只从已有安全文件读取且核对目标；本文件和记忆不保存密钥、密码、DSN或签名链接。

## 4. 必须保留的业务契约

### 模型、权限与协议

- 渠道目录只是推荐，不做名称白名单，不因上游新增/改名拒绝；管理员负责公开模型、映射、分组、价格。
- 不取消用户Token模型授权或管理员路由规则；“去除代码白名单”不等于无权限限制。
- 型号能力交上游验证，不把旧文档时长/素材数/清晰度快照做永久能力表。
- 不能表达的协议输入明确失败，不能静默丢弃素材或伪造支持。
- 名称用于已有协议分支不等于名称准入，也不能用于猜测收费模式。
- 用户继续 `POST /v1/videos`，查询用公开任务ID；客户端Bearer是画境用户令牌，上游Key由渠道提供并私有持久化。
- 上游202/201的局部兼容不能把所有错误响应当成功；客户端成功创建为200+公开ID，私有上游ID不外露。
- URL标量/数组及各适配器支持的对象/role兼容；布尔false、素材顺序、完整签名参数保留。二进制multipart支持不能由JSON URL测试推断。

### 计费

- 按后台**公开模型**选模式/价格，与上游供应商按Token或按次计成本分开。
- snumom启用分辨率每秒计费后，`ModelPriceHelperPerCall` 初始额度0是委托协议，适配器必须随后写正确绝对额度。
- 档位单价×明确正数秒数×分组倍率，只乘一次。估价规格必须与最终发送body一致，metadata冲突不得少收费。
- snumom按秒需要明确 `duration/seconds` 与已配置 `resolution/size`；缺省/-1/冲突/缺价格不猜测。关闭此模式保留原基础价和原缺省行为。
- 当前不是实际Token对账系统。longxia6仍有历史 `-PerSecond` 计费判断，尚未全面迁移到后台显式模式，不能声称所有渠道已统一计费。
- 配额使用共用checked转换/上限/clamp审计，退款依据原任务保存额度；不能改模型名后重新估价既有任务。

### 缓存与取址

- 需要缓存的视频：上游完成后，缓存pending/downloading对外进行中/99%，缓存成功且本地URL可用才成功/100%；失败明确失败。
- kuai 是用户明确要求的无缓存直链例外，不得全局统一为缓存。
- provider parser必须处理完整响应；Anhe/Snumom成功envelope可能被通用model.Task解码吞掉内层URL，需要保留供应商优先解析。
- 当前安和优先真实嵌套内容，包括 `data.data.content.video_url`；没有文档依据不得虚构content下载回退。
- Grok在查询没有model echo时仍按 `url → result_url → video_url`，不能先取残留metadata/content的旧地址。
- 私有ResultURL/上游ID/Key不公开；协议要求鉴权的同源媒体才用任务所选Key（同源不等于自动发Key）。安和当前直链下载及第三方CDN不带渠道Key，跨源重定向清鉴权/Cookie。
- 重试HTTP429/5xx不等于任务终态失败；error:null/空error不等于提交失败。
- 配置下载并发曾要求100，但不是实时配置快照；卡槽要查总超时/取消/槽位回收/补偿，不能用“1G带宽”推断没有瓶颈。

## 5. 渠道编号与 snumom 范围

2026-10-11功能基线 `81580dd69` 的编号快照：kuai65、sd0717 66、wufan67、安和70、manju75、naonao76、manying77、wanchen78、yaochen79、longxia6 80、snumom81，当时Dummy82。2026-10-13官方合入后58–81保持，新增TaskPlugin82/VLLM83/SGLang84，Dummy85仅为数量哨兵；当前完整映射见 [官方合入约定](./upstream-main-integration-2026-10-12.md)。
仓库还有AutoDL H3、Shafu、Diaomao、Zhou SD等已有渠道；不能只保留本窗口重点列表。
官方新增编号如与二开重叠，须设计兼容或经审核的数据迁移，不能直接重排常量让现有渠道走错适配器。

snumom当前推荐21个名称（**不是白名单**）：

- Seedance：`sd-2.5`、`seedance2.5-1`、`seedance2.5-2`、`seedance2-5`、`seedance-2.0-mini`、`seedance-933`、`seedance2.0-933-V`、`seedance2.5`。
- Dreamina：`dreamina-seedance-2-0`、`dreamina-seedance-2-0-fast`、`dreamina-seedance-2-0-mini`、`dreamina-seedance-2-5`。
- Grok：`grok-imagine-video-1.5`、`grok-imagine-video-1.5-fast`、`grok-imagine-video-1.5-lite`。
- H3：`rh-minimaxh3-overseas`、`rh-minimaxh3-pro-overseas`。
- Wan3.0：`wan3.0-video`、`wan3.0-video-prime`、`wan3.0-image`、`wan3.0-image-prime`。

用户先限定截图红框，后来明确追加Dreamina/Grok，不能把所有文档章节都当已授权接入。
Grok音视频/尾帧等不可表达输入仍明确报错；Dreamina首尾帧只作参考排序，不承诺严格插帧。
wanchen只接HN，CB/JL先不接；新增其它协议需要单独确认需求，不能沿用早期草案扩大范围。

## 6. 官方“真实主线”实施与发布边界

2026-10-12用户明确启动真实官方main评估，在完整计划后回复“继续”授权隔离本地实施、合回dev及推送。固定官方目标为 `dd59854050f26f4f67bb3dac5274af5e17013462`，来源 `QuantumNous/new-api`，不跟随浮动main继续滚动。
详见 [本次合入契约与运行边界](./upstream-main-integration-2026-10-12.md)；最终门禁、完整提交和推送证据以工作区Trellis任务 `official-main-preparation` 的 results/final-verification 及Git为准。任务完成后固定官方SHA必须成为dev祖先，临时sync工作树/分支必须清理；缺少这些证据时不能据本段宣告合入完成。
后续2026-10-13检查按用户收窄，仅处理三个正常合入交互，见工作区Trellis `post-merge-deep-check` 的 `integration-contracts.md`、`results.md` 与最终校验收据；不据早期广泛调查继续实施历史/故障或资金架构改造。
本任务不部署、不连接线上数据或Redis、不创建付费生成。历史生产仍按第3节的最后核验日期理解；未来发布需要单独授权、每实例实时预检及一致备份/恢复，不能因为源码已合入就执行code-only升级。
2026-08的 `integrate/upstream-main-20260811`、`e2213109` 是历史基线；现在不按它开日常开发。
本次固定输入为94本地独有/328官方独有提交、330二开文件、118重叠、76文本冲突。历史299提交/1659文件只是当时快照，下一次主线同步仍须重新计算。
旧 `.trellis/tasks/10-11-snumom-upstream-sync/prd.md` 仍为TBD规划占位，不是已审过可直接实施的方案。

后续官方升级仍遵循：

1. 读交接/踩坑/政策，查Git工作树/当前dev/暂停与其它任务边界，不误接Canvas任务。
2. 核对upstream确为QuantumNous/new-api官方地址，fresh fetch main并记录完整SHA、获取时间和merge-base；保存旧dev/生产tag基线。
3. 在必要的临时sync分支/工作树分析官方与二开双方diff，逐模块列出冲突和必须保留的定制。
4. 特别审查渠道编号/注册、TaskAdaptor/relaykit接口、请求重放/重试、原始/映射模型、收费/退款、缓存/取址、登录/邀请/用户/仪表盘及前后端路由契约。
5. 检查GORM字段/默认值/索引/AutoMigrate及已有数据；DB敏感升级不能用code-only标记，需要完整备份、演练和数据库回退方案。
6. 各层解决后跑红→绿和全量回归、relaykit独立构建、前端类型/生产构建、注册和本地mock；不同实际数据库的验证结果分别报告，不能由SQLite推断其它库已测。
7. “完整合入”应核验目标官方SHA成为最终dev的ancestor，明确未合入/有意保留的冲突；挑几个安全提交不能冒充同步官方main完成。
8. 合回dev、全量通过、推送、清理本任务临时分支/工作树；部署必须另外按用户选定target执行技能，阶段审查批准不等于改线上数据/付费调用授权。

## 7. 已有验证与工具陷阱

2026-10-11最终全量 `go test -p 1 ./... -count=1`、`go vet -p 2 ./...` 通过，独立审查通过。
单次Windows HTTP2 GOAWAY测试出现过connection-reset，先定向复查再完整重跑通过；不删测试、不隐藏失败、不据此保证新main不会失败。
本地内存不足时 `pnpm exec tsgo -b --singleThreaded` 通过，`pnpm run build` 通过；Bun优先，未安装时用已有pnpm，不必重装全套依赖。
当前前端是统一 `web/src`，不按旧classic/default双主题打包模板操作。

关键回归入口（命令在代码repo执行）：

```powershell
$env:GOMAXPROCS='2'
$env:GIN_MODE='release'
go test -p 1 ./relay/channel/task/snumom ./relay ./service ./model -count=1
go test -p 1 ./... -count=1
go vet -p 2 ./...
```

重点文件：`relay/snumom_billing_test.go`、`relay/snumom_registration_test.go`、`relay/channel/task/snumom/resolution_billing_test.go`、
`service/anhe_result_url_test.go`、`service/snumom_result_cache_test.go`、`service/media_redirect_test.go`、`service/task_polling_test.go`。
钱包集成测试用playground隔离账户，**不证明Token扣费测试也完成，更不证明真实付费生成已测通**。

发布技能：`D:/dataFromC/.codex/skills/new-api-cos-deploy/SKILL.md`。
只有worker冻结、exact commit测试通过、clean dev==origin/dev后才发布；both一次构建上传、小太阳canary先、画境后。
记录失败/partial rollout；Python临时venv坏了只安全修复确认的工具目录，不能读脚本默认凭据到工具输出。

## 8. 可复制到新窗口的启动说明

> 继续这个New API二开项目。先读规范工作树里的 new-session-handoff、development-pitfalls、conversation-history、AGENTS与渠道长期约定，再核对当前dev/origin/dev和official-main-preparation最终证据。2026-10-12官方目标固定dd598540，本地源码合入与部署状态分别判断；不要重新执行旧TBD任务或把历史生产81580dd69当永远最新dev。未另行授权时不部署、不改线上数据、不创建付费生成；保留管理员模型/价格配置、缓存99→100、Key隔离和双实例隔离。新一轮升级须重新固定官方SHA和完整验证。
