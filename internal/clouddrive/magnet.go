package clouddrive

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

func NormalizeMagnet(raw string) (string, string, error) {
	if len(raw) > 8192 {
		return "", "", errors.New("磁力链接过长")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "magnet" || u.Host != "" || u.Fragment != "" {
		return "", "", errors.New("请输入有效的 magnet 链接")
	}
	var hash string
	for _, xt := range u.Query()["xt"] {
		if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			continue
		}
		value := xt[len("urn:btih:"):]
		var b []byte
		if len(value) == 40 {
			b, err = hex.DecodeString(value)
		} else if len(value) == 32 {
			b, err = base32.StdEncoding.DecodeString(strings.ToUpper(value))
		} else {
			err = errors.New("无效哈希")
		}
		if err != nil || len(b) != 20 {
			return "", "", errors.New("磁力 InfoHash 格式不正确")
		}
		next := hex.EncodeToString(b)
		if hash != "" && next != hash {
			return "", "", errors.New("磁力包含冲突的 InfoHash")
		}
		hash = next
	}
	if hash == "" {
		return "", "", errors.New("磁力缺少 BTIH InfoHash（暂不支持仅 BTv2 的磁力）")
	}
	q := u.Query()
	q.Set("xt", "urn:btih:"+hash)
	u.RawQuery = q.Encode()
	return u.String(), hash, nil
}
