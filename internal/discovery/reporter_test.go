package discovery

import "testing"

func TestAPIURLAddsVersionPathForHostPort(t *testing.T) {
	t.Parallel()

	got := APIURL("vpn.example.com:18080")
	want := "https://vpn.example.com:18080/api/v1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAPIURLPreservesExistingVersionPath(t *testing.T) {
	t.Parallel()

	got := APIURL("https://vpn.example.com:18080/api/v1")
	want := "https://vpn.example.com:18080/api/v1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
