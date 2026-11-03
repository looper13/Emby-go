package cloud115

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
)

type Vault struct {
	aead   cipher.AEAD
	domain []byte
	name   string
}

func OpenVault(filename string) (*Vault, error) {
	return OpenVaultFor(filename, "115")
}

// 不同网盘使用独立密钥和认证域，避免将一方的密文误用于另一方。
func OpenVaultFor(filename, provider string) (*Vault, error) {
	name := "115"
	if provider == "guangya" {
		name = "光鸭"
	} else if provider != "115" {
		return nil, errors.New("不支持的网盘密钥类型")
	}
	if info, err := os.Lstat(filename); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New(name + " 密钥必须是普通文件，不能使用符号链接")
	}
	key, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			if os.IsExist(e) {
				return OpenVaultFor(filename, provider)
			}
			return nil, e
		}
		if e = privateFile(filename); e != nil {
			f.Close()
			return nil, e
		}
		_, e = f.Write(key)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New(name + " 密钥损坏，不能解密既有凭证；请恢复密钥备份")
	}
	if err = privateFile(filename); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	return &Vault{aead: aead, domain: []byte("emby-go/" + provider + "/v1"), name: name}, err
}
func (v *Vault) Seal(t Tokens) ([]byte, error) {
	return v.SealValue(t)
}

func (v *Vault) SealValue(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, b, v.domain), nil
}
func (v *Vault) Open(b []byte) (Tokens, error) {
	var t Tokens
	err := v.OpenValue(b, &t)
	return t, err
}

func (v *Vault) OpenValue(b []byte, value any) error {
	n := v.aead.NonceSize()
	if len(b) < n {
		return errors.New(v.name + " 凭证密文无效")
	}
	plain, err := v.aead.Open(nil, b[:n], b[n:], v.domain)
	if err != nil {
		return errors.New(v.name + " 凭证解密失败，请恢复匹配的密钥")
	}
	return json.Unmarshal(plain, value)
}
