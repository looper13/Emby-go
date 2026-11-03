package cloud115

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultProviderDomainsAndExisting115Format(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".115.key")
	legacy, err := OpenVault(file)
	if err != nil {
		t.Fatal(err)
	}
	tokens := Tokens{Access: "fixture-access", Refresh: "fixture-refresh", Cookie: "fixture-cookie"}
	encrypted, err := legacy.Seal(tokens)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := legacy.Open(encrypted)
	if err != nil || opened != tokens {
		t.Fatal("既有 115 凭据格式无法往返", opened, err)
	}
	// 即便误用了相同密钥，两盘的认证域也必须阻止密文互相解密。
	guangya, err := OpenVaultFor(file, "guangya")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := guangya.OpenValue(encrypted, &value); err == nil {
		t.Fatal("光鸭认证域接受了 115 凭据")
	}
	credentials := map[string]string{"access_token": "fixture-private-access", "device_id": "fixture-device"}
	encrypted, err = guangya.SealValue(credentials)
	if err != nil || strings.Contains(string(encrypted), credentials["access_token"]) {
		t.Fatal("光鸭凭据未受保护", err)
	}
	if err := guangya.OpenValue(encrypted, &value); err != nil || value["access_token"] != credentials["access_token"] {
		t.Fatal("光鸭凭据无法往返", err)
	}
	if _, err := legacy.Open(encrypted); err == nil {
		t.Fatal("115 认证域接受了光鸭凭据")
	}
	key, err := os.ReadFile(file)
	if err != nil || len(key) != 32 {
		t.Fatal("密钥长度不正确", err)
	}
}
