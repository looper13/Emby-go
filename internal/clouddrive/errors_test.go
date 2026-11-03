package clouddrive

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSafeErrorRedactsCloudCredentialsAndPreservesEvidence(t *testing.T) {
	prior := &Error{Message: "光鸭请求失败 fixture-access fixture-refresh https://cdn.invalid/movie?sign=fixture-sign", Auth: true, Missing: true, NotSubmitted: true}
	err := SafeError(fmt.Errorf("包装错误：%w", prior), "fixture-access", "fixture-refresh")
	var safe *Error
	if !errors.As(err, &safe) || !safe.Auth || !safe.Missing || !safe.NotSubmitted || safe.Retryable {
		t.Fatal("脱敏丢失了鉴权、缺失或重试证据", err)
	}
	for _, secret := range []string{"fixture-access", "fixture-refresh", "fixture-sign", "cdn.invalid"} {
		if strings.Contains(safe.Message, secret) {
			t.Fatal("错误信息泄露网盘凭据或临时直链")
		}
	}
	if !strings.Contains(safe.Message, "光鸭") {
		t.Fatal("脱敏丢失网盘品牌")
	}
	unknown := SafeError(errors.New("network unavailable"))
	if !errors.As(unknown, &safe) || safe.Auth || safe.Missing || safe.NotSubmitted || !safe.Retryable {
		t.Fatal("一般网络错误被误判为文件删除或凭据失效", unknown)
	}
}
