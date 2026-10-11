package snumom

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// Resolution billing needs an explicit, unambiguous output specification. Do
// not estimate provider defaults or infer either attribute from a model name.
func resolutionBillingAttributes(request *standardRequest, duration *int) (string, int, *dto.TaskError) {
	seconds := 0
	if duration != nil {
		seconds = *duration
	} else {
		for _, key := range []string{"duration", "seconds"} {
			if value, exists := request.Metadata[key]; exists {
				data, err := common.Marshal(value)
				var parsed flexibleInt
				if err != nil || common.Unmarshal(data, &parsed) != nil {
					return "", 0, invalid("invalid_duration", "resolution pricing requires an explicit integer duration")
				}
				seconds = int(parsed)
				break
			}
		}
	}
	if seconds <= 0 || seconds > relaycommon.MaxTaskDurationSeconds {
		return "", 0, invalid("invalid_duration", fmt.Sprintf("resolution pricing requires an explicit duration between 1 and %d seconds", relaycommon.MaxTaskDurationSeconds))
	}
	requestedResolution := firstNonEmpty(request.Resolution, request.Size)
	metadataResolutionField := ""
	if requestedResolution == "" {
		for _, key := range []string{"resolution", "size"} {
			if value, exists := request.Metadata[key]; exists {
				text, ok := value.(string)
				if !ok {
					return "", 0, invalid("resolution_price_invalid", "resolution pricing requires a string resolution or pixel size")
				}
				requestedResolution = text
				metadataResolutionField = key
				break
			}
		}
	}
	resolution, err := resolutionPricingTier(requestedResolution)
	if err != nil {
		return "", 0, invalid("resolution_price_invalid", err.Error())
	}
	for _, value := range []string{request.Resolution, request.Size} {
		if value == "" {
			continue
		}
		tier, tierErr := resolutionPricingTier(value)
		if tierErr != nil || tier != resolution {
			return "", 0, invalid("resolution_price_invalid", "resolution and size conflict for resolution pricing")
		}
	}
	if err := validateBillingMetadata(request.Metadata, resolution, seconds); err != nil {
		return "", 0, err
	}
	// Direct metadata specifications are supported, but also send the same
	// explicit attributes through the canonical provider protocol. This avoids
	// billing a value that a provider silently ignores in its metadata.
	if request.Resolution == "" && request.Size == "" {
		if metadataResolutionField == "size" {
			request.Size = requestedResolution
		} else {
			request.Resolution = requestedResolution
		}
	}
	if strings.Contains(strings.ToLower(request.Resolution), "x") {
		request.Resolution = resolution
	}
	return resolution, seconds, nil
}

func resolutionPricingTier(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "auto" || value == "adaptive" {
		return "", fmt.Errorf("resolution pricing requires an explicit resolution or pixel size")
	}
	parts := strings.Split(value, "x")
	if len(parts) == 1 {
		return value, nil // Configured tiers, not a model capability allowlist.
	}
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid pixel size for resolution pricing")
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return "", fmt.Errorf("invalid pixel size for resolution pricing")
	}
	return strconv.Itoa(min(width, height)) + "p", nil
}

// Native metadata may contain provider parameters at multiple depths. Every
// forwarded billing attribute must agree with the charge, regardless of which
// source the provider ultimately prioritizes.
func validateBillingMetadata(value any, resolution string, seconds int) *dto.TaskError {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			switch key {
			case "duration", "seconds":
				data, err := common.Marshal(child)
				var parsed flexibleInt
				if err != nil || common.Unmarshal(data, &parsed) != nil || int(parsed) != seconds {
					return invalid("invalid_duration", "metadata duration conflicts with the explicit resolution-pricing duration")
				}
			case "resolution", "size":
				text, ok := child.(string)
				if !ok {
					return invalid("resolution_price_invalid", "metadata resolution must be a string")
				}
				tier, err := resolutionPricingTier(text)
				if err != nil || tier != resolution {
					return invalid("resolution_price_invalid", "metadata resolution conflicts with the explicit resolution-pricing tier")
				}
				if key == "resolution" && strings.Contains(strings.ToLower(text), "x") {
					node[key] = tier
				}
			}
			if err := validateBillingMetadata(child, resolution, seconds); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := validateBillingMetadata(child, resolution, seconds); err != nil {
				return err
			}
		}
	}
	return nil
}
