package taskcommon

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// ResolutionPrice resolves an enabled per-second resolution price for the public model.
func ResolutionPrice(originModel, resolution string) (float64, bool, error) {
	setting, err := model.LoadResolutionPricing(originModel)
	if err != nil {
		return 0, false, err
	}
	if setting == nil {
		return 0, false, nil
	}
	price, err := model.GetResolutionPriceFromSetting(originModel, resolution, setting)
	if err != nil {
		return 0, true, err
	}
	return price, true, nil
}

// ApplyResolutionPerSecondBilling writes an absolute task quota and its audit fields.
func ApplyResolutionPerSecondBilling(c *gin.Context, info *relaycommon.RelayInfo, resolution string, duration int, price float64) {
	quota, clamp := common.QuotaFromFloatChecked(
		price * common.QuotaPerUnit * info.PriceData.GroupRatioInfo.GroupRatio * float64(duration),
	)
	info.PriceData.ModelPrice = price
	info.PriceData.UsePrice = true
	info.PriceData.Quota = quota
	if clamp != nil && info.QuotaClamp == nil {
		info.QuotaClamp = clamp
	}
	c.Set(string(constant.ContextKeyTaskPropsExtra), map[string]interface{}{
		"resolution": resolution,
		"duration":   duration,
	})
}
