package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// An outbound quantity override must reserve additional wallet and token quota.
// If all upstream attempts then fail, the complete reservation is returned once.
func TestImageWalletReservationFailureRefundsFullAmount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		targets []int
	}{
		{name: "initial only"},
		{name: "one quantity increase", targets: []int{300}},
		{name: "multiple quantity increases", targets: []int{300, 600}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			const userID, tokenID, startingWallet = 93001, 93002, 1000
			const startingTokenQuota = 2000
			const tokenKey = "unused-local-wallet-reserve-fixture"
			seedUser(t, userID, startingWallet)
			seedToken(t, tokenID, userID, tokenKey, startingTokenQuota)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{
				UserId: userID, TokenId: tokenID, TokenKey: tokenKey,
				UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
				Request:     &dto.ImageRequest{}, ForcePreConsume: true,
			}
			session, apiErr := NewBillingSession(c, info, 100)
			require.Nil(t, apiErr)
			require.NotNil(t, session)
			for _, target := range tc.targets {
				require.NoError(t, session.Reserve(target))
			}
			reserved := 100
			if len(tc.targets) > 0 {
				reserved = tc.targets[len(tc.targets)-1]
			}
			wallet, err := model.GetUserQuota(userID, true)
			require.NoError(t, err)
			assert.Equal(t, startingWallet-reserved, wallet)
			var token model.Token
			require.NoError(t, model.DB.First(&token, tokenID).Error)
			assert.Equal(t, startingTokenQuota-reserved, token.RemainQuota)
			assert.Equal(t, reserved, token.UsedQuota)

			// The real refund worker updates the token after the wallet. Wait for
			// that database event, rather than sleeping or bypassing the worker.
			completed := make(chan struct{}, 2)
			callback := "wallet_refund_complete_" + tc.name
			require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "tokens" && tx.Error == nil {
					completed <- struct{}{}
				}
			}))
			t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })
			session.Refund(c)
			session.Refund(c)
			select {
			case <-completed:
			case <-time.After(10 * time.Second):
				t.Fatal("refund worker did not complete its token database update")
			}
			wallet, err = model.GetUserQuota(userID, true)
			require.NoError(t, err)
			assert.Equal(t, startingWallet, wallet, "all wallet reservations must be refunded on final failure")
			require.NoError(t, model.DB.First(&token, tokenID).Error)
			assert.Equal(t, startingTokenQuota, token.RemainQuota)
			assert.Zero(t, token.UsedQuota)
			assert.False(t, session.NeedsRefund())
		})
	}
}
