package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveImageSizeTier(t *testing.T) {
	tests := []struct {
		name string
		size string
		want string
	}{
		{name: "explicit 1K", size: "1K", want: "1k"},
		{name: "explicit 2K", size: " 2k ", want: "2k"},
		{name: "explicit 4K", size: "4K", want: "4k"},
		{name: "default size", size: "", want: "1k"},
		{name: "gpt image wide 1K", size: "1584x672", want: "1k"},
		{name: "gpt image portrait 2K", size: "1152x2048", want: "2k"},
		{name: "nano banana wide 2K", size: "3168x1344", want: "2k"},
		{name: "nano banana portrait 4K", size: "3072x5504", want: "4k"},
		{name: "generic at most 1K", size: "800x1024", want: "1k"},
		{name: "generic at most 2K", size: "1200x2000", want: "2k"},
		{name: "generic above 2K", size: "2049x1024", want: "4k"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveImageSizeTier(test.size)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestResolveImageSizeTierRejectsInvalidSize(t *testing.T) {
	for _, size := range []string{"3K", "wide", "0x1024", "1024xabc"} {
		t.Run(size, func(t *testing.T) {
			_, err := ResolveImageSizeTier(size)
			require.Error(t, err)
		})
	}
}

func TestGetImageSizePrice(t *testing.T) {
	testDB := useOptionTestDB(t)
	require.NoError(t, testDB.Create(&Option{
		Key:   ImageSizePriceKey("image-model"),
		Value: `{"enabled":true,"setting":{"1k":0.01,"2k":0.02,"4k":0.04}}`,
	}).Error)

	price, tier, err := GetImageSizePrice("image-model", "3168x1344")

	require.NoError(t, err)
	assert.Equal(t, "2k", tier)
	assert.Equal(t, 0.02, price)
	assert.True(t, HasImageSizePricing("image-model"))
}
