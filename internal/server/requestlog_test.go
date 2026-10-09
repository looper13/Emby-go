package server

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogsRedactCredentialsWithoutChangingRequests(t *testing.T) {
	for _, debug := range []bool{false, true} {
		var output bytes.Buffer
		router := gin.New()
		router.Use(requestLoggerTo(&output))
		if debug {
			router.Use(requestContentLoggerTo(&output))
		}
		body := `{"Username":"test","Pw":"FAKE_PASSWORD","api_key":"FAKE_BODY_KEY"}`
		router.POST("/Users/AuthenticateByName", func(c *gin.Context) {
			if c.Query("api_key") != "FAKE_QUERY_KEY" || c.GetHeader("X-Emby-Token") != "FAKE_HEADER_TOKEN" {
				t.Fatal("redaction changed the request")
			}
			raw, err := io.ReadAll(c.Request.Body)
			if err != nil || string(raw) != body {
				t.Fatal("logger consumed or changed the body")
			}
			c.Status(200)
		})
		request := httptest.NewRequest("POST", "/Users/AuthenticateByName?api_key=FAKE_QUERY_KEY&ApiKey=FAKE_ALT_KEY&X-Emby-Token=FAKE_URL_TOKEN&%61ccess_token=FAKE_ACCESS_TOKEN&limit=10", strings.NewReader(body))
		request.Header.Set("X-Emby-Token", "FAKE_HEADER_TOKEN")
		request.Header.Set("X-MediaBrowser-Token", "FAKE_MEDIA_TOKEN")
		request.Header.Set("Authorization", "Bearer FAKE_AUTHORIZATION")
		request.Header.Set("X-Emby-Authorization", `MediaBrowser Token="FAKE_EMBY_AUTH"`)
		request.Header.Set("Cookie", "token=FAKE_COOKIE")
		router.ServeHTTP(httptest.NewRecorder(), request)
		log := output.String()
		for _, secret := range []string{"FAKE_QUERY_KEY", "FAKE_ALT_KEY", "FAKE_URL_TOKEN", "FAKE_ACCESS_TOKEN", "FAKE_PASSWORD", "FAKE_BODY_KEY", "FAKE_HEADER_TOKEN", "FAKE_MEDIA_TOKEN", "FAKE_AUTHORIZATION", "FAKE_EMBY_AUTH", "FAKE_COOKIE"} {
			if strings.Contains(log, secret) {
				t.Errorf("debug=%v leaked %s", debug, secret)
			}
		}
		if !strings.Contains(log, "limit=10") || !strings.Contains(log, "AuthenticateByName") {
			t.Fatal("nonsecret diagnostic context missing")
		}
	}
}

func TestRequestRecoveryDoesNotLogCredentials(t *testing.T) {
	var output bytes.Buffer
	router := gin.New()
	router.Use(requestLoggerTo(&output), requestRecoveryTo(&output))
	router.GET("/panic", func(c *gin.Context) { panic("FAKE_PANIC_SECRET") })
	request := httptest.NewRequest("GET", "/panic?api_key=FAKE_QUERY_SECRET", nil)
	request.Header.Set("X-Emby-Token", "FAKE_HEADER_SECRET")
	request.Header.Set("Cookie", "key=FAKE_COOKIE_SECRET")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	for _, secret := range []string{"FAKE_PANIC_SECRET", "FAKE_QUERY_SECRET", "FAKE_HEADER_SECRET", "FAKE_COOKIE_SECRET"} {
		if strings.Contains(output.String(), secret) {
			t.Errorf("panic log leaked %s", secret)
		}
	}
	if response.Code != 500 || !strings.Contains(output.String(), "goroutine") {
		t.Fatal("recovery did not preserve response and stack diagnostics")
	}
}

func TestQuietPath(t *testing.T) {
	cases := []struct {
		path  string
		quiet bool
	}{
		{"/web/app.js", true},
		{"/web/vendor/artplayer.min.js", true},
		{"/favicon.ico", true},
		{"/Items/abc/Images/Primary", true},
		{"/emby/Items/abc/Images/Primary/0", true},
		{"/Videos/abc/stream", true},
		{"/Videos/abc/stream.mkv", true},
		{"/emby/videos/abc/proxy", true},
		{"/audio/abc/stream", true},

		// 需要保留记录的接口
		{"/System/Info/Public", false},
		{"/Users/AuthenticateByName", false},
		{"/api/admin/items", false},
		{"/", false},
		{"/Items/abc/PlaybackInfo", false},
		{"/Users/uid/Items", false},
		{"/web", false}, // 管理端首页本体，非静态资源
	}
	for _, tc := range cases {
		if got := quietPath(tc.path); got != tc.quiet {
			t.Errorf("quietPath(%q) = %v，期望 %v", tc.path, got, tc.quiet)
		}
	}
}
