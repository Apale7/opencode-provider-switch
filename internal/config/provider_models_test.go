package config

import (
	"reflect"
	"testing"
)

func TestNormalizeProviderModels(t *testing.T) {
	t.Parallel()

	got := NormalizeProviderModels([]string{" GPT-5.4 ", "gpt-5.4", "", "GPT-5.4"})
	want := []string{"GPT-5.4", "gpt-5.4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeProviderModels() = %#v, want %#v", got, want)
	}
}
