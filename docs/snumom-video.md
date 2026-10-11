# snumom 视频渠道

渠道名 `snumom`，默认地址 `https://snumom.com`，用户侧继续调用标准 `/v1/videos`。
模型目录仅供后台推荐；管理员可配置上游新增模型名和映射，不使用固定模型白名单。

本渠道只推荐截图红框模型：`sd-2.5`、`seedance2.5-1`、`seedance2.5-2`、
`seedance2-5`、`seedance-2.0-mini`、`seedance-933`、`seedance2.0-933-V`、
`seedance2.5`、`rh-minimaxh3-overseas`、`rh-minimaxh3-pro-overseas`，以及
`wan3.0-video`、`wan3.0-video-prime`、`wan3.0-image`、`wan3.0-image-prime`。

适配器把标准图片、视频、音频字符串/数组转换为上游字段。Doubao Token 模型使用
`POST /v1/video/generations`，把素材放入 `metadata.content`，分辨率和画幅也放入
`metadata`；其他模型使用 `/v1/videos`。创建响应的上游任务 ID 仅保存在内部，客户端使用画境公开任务 ID。

轮询完成后优先提取 `metadata.url`、`result_url`、`video_url` 等结果字段进入缓存；缓存前返回进行中，
缓存成功后才返回 100%。按次/按秒应由后台模型价格和计费配置决定，适配器不把推荐模型名当作准入依据。

官方主线同步单独审计：当前 `dev` 与 `upstream/main` 从 2026-08-11 分叉，官方多出 299 个提交、1659 个文件差异，
涉及核心后端、relay、前端和定制渠道。为保持现有二开功能，本轮未整体合并官方主线；后续应按安全、支付、relaykit、
前端等领域选择性移植并逐层回归。
