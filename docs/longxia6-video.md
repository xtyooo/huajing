# longxia6 视频渠道

> 型号能力表已从本地校验移除，下表仅为上游文档参考，实际能力由上游判断。
> `-PerSecond` 后缀计费仍为历史行为，待迁移为后台显式模式。
> 后续接入遵循[渠道接入长期约定](./channel-integration-policy.md)。

后台新增渠道选择 `longxia6`（编号80），默认地址
`https://api8.longxiaai.store`，填写上游客户 Key。
模型可以从上游获取或手动配置，支持后台模型映射；模型名校验不使用固定白名单。
本次按提供文档接视频模型，不增加图片生成模式。

## 能力和计价

| 型号系列 | 时长 | 图片/音频/视频上限 | 比例 |
| --- | --- | --- | --- |
| Seedance2.0 Mini/Fast/Standard | 4–15秒 | 6/2/0 | 9:16、16:9 |
| Seedance2.5 Standard480p/720p | 4–25秒 | 30/10/0 | 六种比例 |
| Seedance2.5 720p reference-video | 4–15秒 | 30/10/10 | 六种比例 |
| MiniMax H3 | 5–15秒 | 5/3/0 | 六种比例 |
| Gemini Omni Flash | 3–10秒 | 1–5图，0音频/视频 | 9:16、16:9 |

六种比例为 9:16、16:9、1:1、4:3、3:4、21:9。
默认8秒、9:16；分辨率与音轨由型号决定。网关不把 `resolution`、
`generate_audio` 等标准可选项发送到不接受这些字段的上游。
本地不再按型号限制提示词长度、素材数量、画幅、组合和生成时长；
仅保留公共计费时长安全上限、素材结构/安全/编号校验。库存提示不作为禁用规则。

普通型号：后台“按请求计费（固定价格）”填写一次生成价格。
映射后的上游型号以 `-PerSecond` 结尾：该位置填写每秒基础价格，
适配器提供 seconds 倍率；如果后台为公开模型设置固定总价并开启
“跳过秒数计算”，则不叠加时长。分组倍率沿用现有规则。
本渠道不使用 Resolution per second 配置；误启用时明确拒绝，避免零计费。
所有价格使用后台配置，不跟随文档价格自动写入。

## 用户请求

用户仍用画境 Key 请求 `POST /v1/videos`：

```json
{
  "model": "LongXia-video-seedance2_5-standard-720p-reference-video-express",
  "prompt": "让 @image1 中的人物参考 @video1 的动作，跟随 @audio1 的节奏",
  "seconds": "8",
  "aspect_ratio": "16:9",
  "reference_image_urls": ["https://example.com/person.png"],
  "reference_videos": ["https://example.com/motion.mp4"],
  "audio_urls": ["https://example.com/voice.mp3"]
}
```

标准图片/视频/音频列表支持单个字符串或数组。按类别保留素材顺序，
转换为上游 `assets:[{category,url}]`；Data URI 转为不带前缀的
`data_base64`。也支持直接提供规范的 `assets`。
上游只收到 model、prompt、duration（整数）、size（比例）、assets。

提示词需要明确引用每个素材：`@image1`、`@image2`、
`@audio1`、`@video1` 等。不能漏号或引用不存在的素材；
网关不会自动改写提示词。音频应同时搭配图片。
首尾帧模式不在本次上游协议中，不能将两张参考图当作首尾帧。

公开素材使用HTTPS链接；图片PNG/JPEG/WebP，参考音频MP3。
远程实际格式、大小、时长及总素材64MiB限制由上游异步检测；
网关在提交前校验结构、编号和内联Base64大小，避免提交时下载大文件。
视频参考是否可用以及每类素材数量由上游按实际型号能力判断。

## 查询与缓存

- `GET /v1/videos/{画境任务ID}` 查询；用户不使用上游私有任务ID。
- 从上游 `data[]` 选择视频结果；明确图片结果不当视频缓存。
- 完成却无有效视频地址时明确失败，不虚构未声明的 /content。
- 结果直链有效期文档标注24小时，因此仍默认缓存到画境：
  缓存期间进行中/99%，缓存成功才成功/100%并给出画境URL。
- CDN下载不附带渠道Key。

验证使用模拟上游；真实生成需后台配置可用Key及价格。
