package kitutil

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	maskURLPattern    = regexp.MustCompile(`(http|https)://[^\s/$.?#].[^\s]*`)
	maskDomainPattern = regexp.MustCompile(`\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\b`)
	maskIPPattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// maskApiKeyPattern 匹配 api_key:xxx 形式的文本，避免上游密钥出现在公开错误中。
	maskApiKeyPattern = regexp.MustCompile(`(['"]?)api_key:([^\s'"]+)(['"]?)`)
	// maskRequiredQuotaAmountPattern 只匹配明确业务标签后的额度数值，避免误伤用户余额和其他价格。
	maskRequiredQuotaAmountPattern = regexp.MustCompile(`(需要预扣费额度\s*[:：]\s*)[^0-9"'\\,，\r\n}\]\(]*[+-]?(?:(?:[0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(?:\.[0-9]+)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)
)

// maskHostTail 返回域名脱敏后应保留的后缀部分；国家或地区域名保留两段，其他域名仅保留顶级域。
func maskHostTail(parts []string) []string {
	if len(parts) < 2 {
		return parts
	}
	lastPart := parts[len(parts)-1]
	secondLastPart := parts[len(parts)-2]
	if len(lastPart) == 2 && len(secondLastPart) <= 3 {
		// co.uk、com.cn 等国家或地区域名需要保留两段后缀，便于定位上游类型。
		return []string{secondLastPart, lastPart}
	}
	return []string{lastPart}
}

// maskHostForURL 隐藏 URL 中的完整主机名，仅保留可识别的域名后缀。
func maskHostForURL(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "***"
	}
	tail := maskHostTail(parts)
	return "***." + strings.Join(tail, ".")
}

// maskHostForPlainDomain 隐藏普通域名，并用星号段数保留原有域名层级信息。
func maskHostForPlainDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return domain
	}
	tail := maskHostTail(parts)
	numStars := len(parts) - len(tail)
	if numStars < 1 {
		numStars = 1
	}
	stars := strings.TrimSuffix(strings.Repeat("***.", numStars), ".")
	return stars + "." + strings.Join(tail, ".")
}

// MaskRequiredQuotaAmount 隐藏余额不足错误中的实际所需额度，同时保留余额、请求编号和响应结构。
func MaskRequiredQuotaAmount(str string) string {
	return maskRequiredQuotaAmountPattern.ReplaceAllString(str, "${1}****")
}

// MaskSensitiveInfo 隐藏错误文本中的额度、URL、IP、域名和 API Key，同时保留排障所需的结构信息。
func MaskSensitiveInfo(str string) string {
	// 业务额度先独立脱敏，确保普通文本、OpenAI/Claude 错误和嵌套 JSON 都走同一规则。
	str = MaskRequiredQuotaAmount(str)

	// URL 仅保留协议和域名后缀，路径与查询参数值统一隐藏。
	str = maskURLPattern.ReplaceAllStringFunc(str, func(urlStr string) string {
		u, err := url.Parse(urlStr)
		if err != nil {
			return urlStr
		}

		host := u.Host
		if host == "" {
			return urlStr
		}

		// 主机名统一走域名脱敏逻辑，避免 URL 与普通域名的规则产生偏差。
		maskedHost := maskHostForURL(host)

		result := u.Scheme + "://" + maskedHost

		// 路径仅保留层级，避免资源标识或用户数据泄露。
		if u.Path != "" && u.Path != "/" {
			pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
			maskedPathParts := make([]string, len(pathParts))
			for i := range pathParts {
				if pathParts[i] != "" {
					maskedPathParts[i] = "***"
				}
			}
			if len(maskedPathParts) > 0 {
				result += "/" + strings.Join(maskedPathParts, "/")
			}
		} else if u.Path == "/" {
			result += "/"
		}

		// 查询参数保留参数名但隐藏参数值，兼顾排障和敏感信息保护。
		if u.RawQuery != "" {
			values, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				// 无法解析查询参数时隐藏完整查询串，避免异常格式绕过脱敏。
				result += "?***"
			} else {
				maskedParams := make([]string, 0, len(values))
				for key := range values {
					maskedParams = append(maskedParams, key+"=***")
				}
				if len(maskedParams) > 0 {
					result += "?" + strings.Join(maskedParams, "&")
				}
			}
		}

		return result
	})

	// 对不带协议的普通域名执行同等脱敏。
	str = maskDomainPattern.ReplaceAllStringFunc(str, func(domain string) string {
		return maskHostForPlainDomain(domain)
	})

	// IP 地址不保留任何网段信息。
	str = maskIPPattern.ReplaceAllString(str, "***.***.***.***")

	// API Key 仅保留字段名，便于确认错误来源。
	str = maskApiKeyPattern.ReplaceAllString(str, "${1}api_key:***${3}")

	return str
}
