# snumom 视频渠道

渠道名 `snumom`，默认地址 `https://snumom.com`，用户侧继续调用标准 `/v1/videos`。
模型目录仅供后台推荐；管理员可配置上游新增模型名和映射，不使用固定模型白名单。

本渠道此前已接入截图红框模型：`sd-2.5`、`seedance2.5-1`、`seedance2.5-2`、
`seedance2-5`、`seedance-2.0-mini`、`seedance-933`、`seedance2.0-933-V`、
`seedance2.5`、`rh-minimaxh3-overseas`、`rh-minimaxh3-pro-overseas`，以及
`wan3.0-video`、`wan3.0-video-prime`、`wan3.0-image`、`wan3.0-image-prime`。

2026-10-11 追加的两个系列：

| 系列 | 上游模型 ID |
| --- | --- |
| Dreamina Seedance | `dreamina-seedance-2-0`、`dreamina-seedance-2-0-fast`、`dreamina-seedance-2-0-mini`、`dreamina-seedance-2-5` |
| Grok 视频 | `grok-imagine-video-1.5`、`grok-imagine-video-1.5-fast`、`grok-imagine-video-1.5-lite` |

这些目录仍不是请求白名单；新名称需由管理员添加模型及映射，实际能力由上游判断。

## Dreamina 请求

使用标准 JSON `POST /v1/videos`，四个型号共享素材格式。
文档时长/清晰度等型号能力由上游验证，网关保留类型和通用资源边界。

```json
{
  "model": "dreamina-seedance-2-0",
  "prompt": "@image1 的人物参考 @video1 的动作和 @audio1 的节奏",
  "seconds": "8",
  "aspect_ratio": "16:9",
  "resolution": "1080p",
  "reference_image_urls": ["https://example.com/person.jpg"],
  "reference_videos": ["https://example.com/action.mp4"],
  "audio_urls": ["https://example.com/voice.mp3"]
}
```

画境标准图片/视频/音频别名转换为 `reference_images/reference_videos/reference_audios`。
接受单个 URL、URL 数组、带 `url` 的素材对象及 `content` 多模态结构；
提示词缺省时可取 `content` 的文本。首帧/尾帧角色按文档排列图片参考，
这一组模型不承诺严格的首尾帧生成。
素材为公网 HTTP(S) 直链；图片可用 Data URI，参考视频/音频使用 URL。

## Grok 请求

同样使用标准 JSON `POST /v1/videos`，三个型号共享图片参考格式。
上游识别的是 `reference_images` 或 `input_reference`，不能原样发送它会忽略的
`image/image_url` 字段；画境会把这些兼容输入转换到正确位置。

```json
{
  "model": "grok-imagine-video-1.5-fast",
  "prompt": "保持参考人物外观一致，镜头缓慢推进",
  "duration": 8,
  "resolution": "720p",
  "ratio": "16:9",
  "reference_images": [
    {"url": "https://example.com/person.jpg", "role": "reference_image"}
  ]
}
```

上游收到字符串 `seconds`、`size`、`aspect_ratio` 和 `reference_images`。
Grok 图片保留 `reference_image/first_frame` 角色；
尾帧、参考音频、参考视频在该协议中无法表达，明确报参数错误，不静默丢弃。
完成取址按文档优先 `url/result_url/video_url`，保留签名查询参数。
本次只支持 JSON 素材 URL 方式，不承诺 multipart 文件上传。

## 价格配置与验证边界

画境侧价格从后台**公开模型**配置读取，不硬编码金额，也不根据映射后的上游型号名称推断计费方式。
上游模型改名不应改变画境收费；现有生产价格配置不会由程序自动修改。

- 未启用分辨率定价：继续使用原有基础任务价格；按请求固定价格不自动叠加时长。
- 启用 `Resolution (per second)`：根据请求的分辨率档位读取后台每秒单价，
  费用为 `每秒单价 × duration/seconds × 分组倍率`，使用共用预扣与任务结算逻辑，秒数只乘一次。
- 按秒模式需要明确的正整数 `duration` 或 `seconds` 及可对应后台价格的 `resolution/size`；
  时长缺失、自动时长 `-1`、不一致的透传规格或价格档位缺失会在提交前明确报错，不能猜测或零价提交。
  字符串/数字秒数、清晰度大小写和常见像素尺寸会归一化；关闭分辨率定价时不新增这些必填限制。

例如后台设置 480p/720p/1080p 每秒分别为 `$0.23/$0.28/$0.33`，生成10秒视频、
分组倍率为1时，对应 `$2.30/$2.80/$3.30`。消费日志记录所用分辨率、时长和档位单价。

Dreamina 文档的上游 Token 成本与画境用户侧售价是独立配置。本次支持固定任务价格或
上述按分辨率每秒售价，**没有实现实际 Token 用量结算或上游 Token 账单自动对账**。

适配器把标准图片、视频、音频字符串/数组转换为上游字段。Doubao Token 模型使用
`POST /v1/video/generations`，把素材放入 `metadata.content`，分辨率和画幅也放入
`metadata`；其他模型使用 `/v1/videos`。创建响应的上游任务 ID 仅保存在内部，客户端使用画境公开任务 ID。

轮询完成后提取对应协议的直链进入缓存；缓存前返回进行中/99%，
缓存成功后才返回100%和画境URL。任务所选密钥只发往同源视频地址，
跨源跳转移除鉴权，第三方CDN不携带渠道Key。
`metadata` 参数保持嵌套，不摊到顶层；生成任务公开ID与上游ID隔离。
验证使用模拟上游，未创建真实付费生成任务。

官方主线同步单独审计：当前 `dev` 与 `upstream/main` 从 2026-08-11 分叉，官方多出 299 个提交、1659 个文件差异，
涉及核心后端、relay、前端和定制渠道。为保持现有二开功能，本轮未整体合并官方主线；后续应按安全、支付、relaykit、
前端等领域选择性移植并逐层回归。
