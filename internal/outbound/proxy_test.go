package outbound

import "testing"

func TestRemoveProxyNormalizesAndRejectsMissing(t *testing.T) {
	if err := ConfigurePool([]string{"http://example.com/"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProxy("http://example.com"); err != nil {
		t.Fatal(err)
	}
	if len(ProxyPoolStatus()) != 0 {
		t.Fatalf("pool not empty: %#v", ProxyPoolStatus())
	}
	if err := RemoveProxy("http://missing.example"); err == nil {
		t.Fatal("expected missing proxy error")
	}
}
