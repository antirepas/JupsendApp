package util

import "testing"

func TestStaticAssetVersion(t *testing.T) {
	v1 := StaticAssetVersion()
	v2 := StaticAssetVersion()
	if v1 == "" {
		t.Fatal("expected non-empty asset version")
	}
	if v1 != v2 {
		t.Fatalf("expected stable version, got %q and %q", v1, v2)
	}
}
