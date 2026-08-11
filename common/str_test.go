package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMaskRequiredQuotaAmount 验证仅隐藏所需预扣额度，并保留余额、请求编号及原始响应结构。
func TestMaskRequiredQuotaAmount(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "普通文本",
			input:    "预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ¥3.520000",
			expected: "预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ****",
		},
		{
			name:     "全角标点和带空格币种",
			input:    "预扣费额度失败，用户剩余额度：💵4.642578，需要预扣费额度：US$ 3.520000 (request id: req-1)",
			expected: "预扣费额度失败，用户剩余额度：💵4.642578，需要预扣费额度：**** (request id: req-1)",
		},
		{
			name:     "任务响应 JSON",
			input:    `{"code":"insufficient_user_quota","message":"预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ＄3.520000","data":null}`,
			expected: `{"code":"insufficient_user_quota","message":"预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ****","data":null}`,
		},
		{
			name:     "转义后的嵌套任务响应",
			input:    `{\"code\":\"fail_to_fetch_task\",\"message\":\"{\\\"code\\\":\\\"insufficient_user_quota\\\",\\\"message\\\":\\\"需要预扣费额度: ¥3.520000\\\"}\"}`,
			expected: `{\"code\":\"fail_to_fetch_task\",\"message\":\"{\\\"code\\\":\\\"insufficient_user_quota\\\",\\\"message\\\":\\\"需要预扣费额度: ****\\\"}\"}`,
		},
		{
			name:     "纯数字额度",
			input:    "预扣费额度失败, 用户剩余额度: 918, 需要预扣费额度: 3520",
			expected: "预扣费额度失败, 用户剩余额度: 918, 需要预扣费额度: ****",
		},
		{
			name:     "已经完成脱敏",
			input:    "预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ****",
			expected: "预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ****",
		},
		{
			name:     "不相关金额保持原样",
			input:    "模型价格配置错误, 当前价格: ¥3.520000, 用户剩余额度: ¥0.918000",
			expected: "模型价格配置错误, 当前价格: ¥3.520000, 用户剩余额度: ¥0.918000",
		},
		{
			name:     "没有金额的相似文案保持原样",
			input:    "需要预扣费额度计算失败",
			expected: "需要预扣费额度计算失败",
		},
		{
			name:     "冒号后没有金额时保持原样",
			input:    "需要预扣费额度: 计算失败",
			expected: "需要预扣费额度: 计算失败",
		},
		{
			name:     "金额后的请求编号保持原样",
			input:    "需要预扣费额度: ¥3.520000 request id: req-1",
			expected: "需要预扣费额度: **** request id: req-1",
		},
		{
			name:     "金额后的备注保持原样",
			input:    "需要预扣费额度: ¥3.520000; retry disabled",
			expected: "需要预扣费额度: ****; retry disabled",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, MaskRequiredQuotaAmount(testCase.input))
			assert.Equal(t, testCase.expected, MaskSensitiveInfo(testCase.input))
		})
	}
}
