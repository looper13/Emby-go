package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"emby-go/internal/probe"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

// FFmpeg 和 ffprobe 使用本机短期代理输入，全部回源仍经过统一的请求头和重定向边界。
func (a *App) proxyMediaInput(ctx context.Context, movie store.Movie, id, raw string) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	library, err := a.db.Library(movie.LibraryID)
	if err != nil || !library.Enabled {
		return "", nil, errors.New("媒体库不可用")
	}
	if !library.IsDirectory() || !library.ProxyEnabled {
		return raw, func() {}, nil
	}
	capability := randomSessionID()
	if capability == "" {
		return "", nil, errors.New("生成播放输入能力失败")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, errors.New("创建本机播放输入失败")
	}
	// 单次输入独立管理 HLS 会话，后台探测不会挤占网页播放的会话数量。
	owner := &App{db: a.db, appState: &appState{rootCtx: ctx}}
	router := gin.New()
	inputPath := "/" + capability + "/input"
	handler := func(c *gin.Context) {
		current, _, _, valid := owner.resolvePlaybackTarget(id)
		currentLibrary, readErr := owner.db.Library(movie.LibraryID)
		if !valid || !current.IsVisible() || current.LibraryID != movie.LibraryID || readErr != nil || !currentLibrary.Enabled || !currentLibrary.ProxyEnabled {
			c.Status(http.StatusNotFound)
			return
		}
		c.Params = append(c.Params, gin.Param{Key: "id", Value: id})
		settings := playbackProxySettings(currentLibrary, raw)
		settings.movieID = current.ID
		owner.proxyRemoteWithHLSSession(c, raw, "", settings)
	}
	router.GET(inputPath, handler)
	router.HEAD(inputPath, handler)
	for _, base := range []string{"/Videos/:id/hls-proxy/:session/:resource", "/videos/:id/hls-proxy/:session/:resource"} {
		router.GET(base, owner.hlsProxyResource)
		router.HEAD(base, owner.hlsProxyResource)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	cleanup := func() {
		stop()
		_ = server.Close()
		_ = listener.Close()
	}
	return "http://" + listener.Addr().String() + inputPath, cleanup, nil
}

func (a *App) runMediaProbe(ctx context.Context, ffprobe, raw string, target probeTarget) (probe.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, a.configSnapshot().ProbeTimeout())
	defer cancel()
	id := strconv.FormatInt(target.Movie.ID, 10)
	if !target.Primary {
		id = virtualPartID(target.Movie.ID, target.Index)
	}
	input, cleanup, err := a.proxyMediaInput(ctx, target.Movie, id, raw)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return probe.Result{}, contextErr
		}
		return probe.Result{}, err
	}
	defer cleanup()
	return probe.Run(ctx, ffprobe, input, a.configSnapshot().ProbeTimeout())
}
