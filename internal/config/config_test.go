package config

import "testing"

func TestValidateProviderBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "valid exact", input: "https://example.com/v1"},
		{name: "valid trailing slash", input: "https://example.com/v1/"},
		{name: "valid trimmed", input: "  https://example.com/v1/  "},
		{name: "missing", input: "", wantErr: "missing base_url"},
		{name: "missing v1", input: "https://example.com/api", wantErr: "base_url must end with /v1"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProviderBaseURL(tt.input)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}
