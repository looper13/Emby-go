package cloud115

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
)

type Vault struct{ aead cipher.AEAD }

func OpenVault(filename string) (*Vault, error) {
	if info, err := os.Lstat(filename); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("115 密钥必须是普通文件，不能使用符号链接")
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
				return OpenVault(filename)
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
		return nil, errors.New("115 密钥损坏，不能解密既有凭证；请恢复密钥备份")
	}
	if err = privateFile(filename); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	return &Vault{aead}, err
}
func (v *Vault) Seal(t Tokens) ([]byte, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, b, []byte("emby-go/115/v1")), nil
}
func (v *Vault) Open(b []byte) (Tokens, error) {
	var t Tokens
	n := v.aead.NonceSize()
	if len(b) < n {
		return t, errors.New("115 凭证密文无效")
	}
	plain, err := v.aead.Open(nil, b[:n], b[n:], []byte("emby-go/115/v1"))
	if err != nil {
		return t, errors.New("115 凭证解密失败，请恢复匹配的密钥")
	}
	err = json.Unmarshal(plain, &t)
	return t, err
}
