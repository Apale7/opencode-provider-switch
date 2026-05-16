package version

import "testing"

func TestResolveUsesInjectedVersion(t *testing.T) {
	t.Parallel()

	if got := Resolve(" v1.2.3 "); got != "v1.2.3" {
		t.Fatalf("Resolve(injected) = %q, want v1.2.3", got)
	}
}

func TestResolveFallsBackForDev(t *testing.T) {
	t.Parallel()

	got := Resolve("dev")
	if got == "" {
		t.Fatal("Resolve(dev) returned empty version")
	}
}
