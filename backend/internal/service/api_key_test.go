package service

import (
	"testing"
)

func TestAPIKey_IsTeamMode(t *testing.T) {
	tests := []struct {
		name string
		key  *APIKey
		want bool
	}{
		{
			name: "key with TeamID set returns true",
			key: func() *APIKey {
				tid := int64(42)
				return &APIKey{TeamID: &tid}
			}(),
			want: true,
		},
		{
			name: "key with nil TeamID returns false",
			key:  &APIKey{},
			want: false,
		},
		{
			name: "nil key returns false",
			key:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.key.IsTeamMode()
			if got != tt.want {
				t.Errorf("IsTeamMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
