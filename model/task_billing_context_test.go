package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskPrivateDataImageSizeBillingContextRoundTrip(t *testing.T) {
	original := TaskPrivateData{BillingContext: &TaskBillingContext{
		ModelPrice:       0.04,
		GroupRatio:       1.5,
		OriginModelName:  "async-image-model",
		PerCallBilling:   true,
		ImageSizePricing: true,
		ImageSizeTier:    "4k",
	}}

	value, err := original.Value()
	require.NoError(t, err)
	bytesValue, ok := value.([]byte)
	require.True(t, ok)
	var restored TaskPrivateData
	require.NoError(t, restored.Scan(bytesValue))
	require.NotNil(t, restored.BillingContext)
	assert.Equal(t, original.BillingContext, restored.BillingContext)
}

func TestTaskPrivateDataLegacyBillingContextRemainsCompatible(t *testing.T) {
	var restored TaskPrivateData
	require.NoError(t, restored.Scan([]byte(`{"billing_context":{"model_price":0.02,"origin_model_name":"video-model","per_call_billing":true}}`)))
	require.NotNil(t, restored.BillingContext)
	assert.False(t, restored.BillingContext.ImageSizePricing)
	assert.Empty(t, restored.BillingContext.ImageSizeTier)
}
