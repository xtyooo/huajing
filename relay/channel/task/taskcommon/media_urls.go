package taskcommon

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// MediaURLList accepts a single URL or a URL array at the client boundary.
// It preserves order and URL contents; provider validation still applies.
type MediaURLList []string

func (urls *MediaURLList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*urls = nil
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := common.Unmarshal(data, &value); err != nil {
			return err
		}
		if strings.TrimSpace(value) == "" {
			*urls = nil
		} else {
			*urls = MediaURLList{value}
		}
		return nil
	}
	// Pointer elements distinguish null from an empty string, preventing silent
	// acceptance of malformed references in an otherwise valid array.
	var values []*string
	if err := common.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("reference media must be a URL string or an array of URL strings: %w", err)
	}
	result := make(MediaURLList, len(values))
	for i, value := range values {
		if value == nil {
			return fmt.Errorf("reference media array item %d must be a string", i)
		}
		result[i] = *value
	}
	*urls = result
	return nil
}
