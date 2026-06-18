package vless

import "testing"

func TestParseRealityVision(t *testing.T) {
	node, err := Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Example")
	if err != nil {
		t.Fatal(err)
	}
	if node.ID != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("unexpected id %q", node.ID)
	}
	if node.Host != "example.com" || node.Port != 443 {
		t.Fatalf("unexpected endpoint %s:%d", node.Host, node.Port)
	}
	if node.DisplayName() != "Example" {
		t.Fatalf("unexpected display name %q", node.DisplayName())
	}
	if node.Flow() != "xtls-rprx-vision" {
		t.Fatalf("unexpected flow %q", node.Flow())
	}
	if node.Network() != "tcp" {
		t.Fatalf("unexpected network %q", node.Network())
	}
	if err := node.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestParseRejectsInvalidUUID(t *testing.T) {
	if _, err := Parse("vless://not-a-uuid@example.com:443?encryption=none#Example"); err == nil {
		t.Fatal("expected invalid UUID to be rejected")
	}
}

func TestValidateRejectsInvalidRealityPublicKey(t *testing.T) {
	node, err := Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&security=reality&sni=www.example.com&fp=chrome&pbk=abc&sid=0123&type=tcp#Example")
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Validate(); err == nil {
		t.Fatal("expected invalid REALITY public key to be rejected")
	}
}
