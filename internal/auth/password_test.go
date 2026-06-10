package auth

import (
	"errors"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{
			name:    "11 runes → ErrPasswordTooShort",
			input:   "abcdefghijk", // 11 runes
			wantErr: ErrPasswordTooShort,
		},
		{
			name:    "12 runes → nil",
			input:   "abcdefghijkl", // 12 runes
			wantErr: nil,
		},
		{
			name:    "empty → ErrPasswordTooShort",
			input:   "",
			wantErr: ErrPasswordTooShort,
		},
		{
			name: "72 bytes → nil",
			// 72 ASCII chars = 72 bytes exactly
			input:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: nil,
		},
		{
			name: "73 bytes → ErrPasswordTooLong",
			// 73 ASCII chars = 73 bytes
			input:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: ErrPasswordTooLong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidatePassword(%q) = %v; want %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
