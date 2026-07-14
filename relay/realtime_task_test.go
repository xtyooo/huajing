package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestCanPersistRealtimeTaskStatus(t *testing.T) {
	tests := []struct {
		name   string
		status model.TaskStatus
		want   bool
	}{
		{name: "submitted", status: model.TaskStatusSubmitted, want: true},
		{name: "queued", status: model.TaskStatusQueued, want: true},
		{name: "in progress", status: model.TaskStatusInProgress, want: true},
		{name: "success", status: model.TaskStatusSuccess, want: false},
		{name: "failure", status: model.TaskStatusFailure, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, canPersistRealtimeTaskStatus(tt.status))
		})
	}
}
