package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/scanner"
	"emby-go/internal/subtitle"
	"github.com/gin-gonic/gin"
)

type audioSession struct {
	userID        int64
	item, dir     string
	clientSession string
	cancel        context.CancelFunc
	done          chan struct{}
	touched       atomic.Int64
}

func playbackWorkers(cfg config.Config) int {
	if cfg.PlaybackConcurrency <= 0 {
		return 2
	}
	return min(cfg.PlaybackConcurrency, 4)
}

func playbackCacheMB(cfg config.Config) int {
	if cfg.PlaybackCacheMB <= 0 {
		return 2048
	}
	return cfg.PlaybackCacheMB
}

func (a *App) acquireMediaProcess() (func(), bool) {
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	if a.rootCtx.Err() != nil || a.mediaProcesses >= playbackWorkers(a.configSnapshot()) {
		return nil, false
	}
	a.mediaProcesses++
	a.mediaWG.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() { a.mediaMu.Lock(); a.mediaProcesses--; a.mediaMu.Unlock(); a.mediaWG.Done() })
	}, true
}

func (a *App) ffmpegExecutable() (string, error) {
	cfg := a.configSnapshot()
	if !cfg.FFmpegEnabled {
		return "", errors.New("请先在系统配置中启用 FFmpeg 播放处理")
	}
	path := strings.TrimSpace(cfg.FFmpegPath)
	if path == "" {
		path = "ffmpeg"
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", errors.New("未找到 FFmpeg，请检查系统配置中的路径")
	}
	return resolved, nil
}

type limitedBuffer struct {
	data []byte
	max  int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.max {
		return 0, errors.New("字幕提取结果过大")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func (a *App) extractSubtitle(c *gin.Context, source string, index int, format string) {
	if format != "vtt" && format != "srt" && format != "ass" && format != "ssa" {
		c.Status(415)
		return
	}
	program, err := a.ffmpegExecutable()
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	key := fmt.Sprintf("%x:%d:%s", sha256.Sum256([]byte(source)), index, format)
	a.mediaMu.Lock()
	if a.subtitleBytes == nil {
		a.subtitleBytes = cache.NewMemory(8)
	}
	data, hit := a.subtitleBytes.Get(key)
	a.mediaMu.Unlock()
	if hit {
		c.Data(200, subtitle.MIME(format), data)
		return
	}
	release, ok := a.acquireMediaProcess()
	if !ok {
		c.JSON(429, gin.H{"error": "播放处理繁忙，请稍后重试"})
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(c.Request.Context(), a.configSnapshot().ProbeTimeout())
	defer cancel()
	stop := context.AfterFunc(a.rootCtx, cancel)
	defer stop()
	mux := map[string]string{"vtt": "webvtt", "srt": "srt", "ass": "ass", "ssa": "ass"}[format]
	cmd := exec.CommandContext(ctx, program, "-nostdin", "-v", "error", "-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy", "-i", source,
		"-map", fmt.Sprintf("0:%d", index), "-f", mux, "pipe:1")
	output := &limitedBuffer{max: subtitle.MaxFileSize}
	cmd.Stdout, cmd.Stderr = output, io.Discard
	if err := cmd.Run(); err != nil {
		c.JSON(502, gin.H{"error": "内封字幕提取失败，请检查源地址、字幕格式或超时配置"})
		return
	}
	a.subtitleBytes.Set(key, output.data, 10*time.Minute)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, subtitle.MIME(format), output.data)
}

func (a *App) startAudioPlayback(c *gin.Context) {
	program, err := a.ffmpegExecutable()
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	var request struct {
		AudioStreamIndex int
		StartTimeTicks   int64
		PreviousSession  string
		PlaySessionId    string
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if c.ShouldBindJSON(&request) != nil || request.StartTimeTicks < 0 {
		c.Status(400)
		return
	}
	movie, path, id, ok := a.resolvePlaybackTarget(c.Param("id"))
	if !ok || !movie.IsVisible() {
		c.Status(404)
		return
	}
	raw, err := scanner.ReadSource(path)
	if err != nil || !scanner.ValidHTTP(raw) {
		c.Status(404)
		return
	}
	if movie.RuntimeSeconds > 0 && request.StartTimeTicks >= movie.RuntimeSeconds*10000000 {
		c.Status(400)
		return
	}
	a.enableDetailTracks(c, movie, path)
	value, _ := c.Get("detailTracks:" + path)
	tracks := a.resolveDetailTracks(movie, path, value.(trackSnapshot))
	valid := false
	external := false
	audioCodec := ""
	externalPath := ""
	containerIndex := -1
	for _, track := range tracks {
		stream := track.stream
		if stream["Type"] == "Audio" && stream["Index"] == request.AudioStreamIndex && stream["ContainerIndexKnown"] == true {
			valid = true
			external = stream["IsExternal"] == true
			audioCodec, _ = stream["Codec"].(string)
			if track.file != nil {
				externalPath, err = subtitle.ResolveLocal(path, track.file.Path)
			} else {
				containerIndex, _ = stream["ContainerIndex"].(int)
			}
			break
		}
	}
	if !valid {
		c.JSON(400, gin.H{"error": "音轨不存在或媒体源已更换，请刷新播放信息"})
		return
	}
	if external && (err != nil || externalPath == "") {
		c.JSON(404, gin.H{"error": "外挂音频文件不可用"})
		return
	}
	if request.PreviousSession != "" {
		a.stopMediaSession(id, request.PreviousSession)
	}
	if request.PlaySessionId != "" {
		var previous string
		a.mediaMu.Lock()
		for key, session := range a.mediaSessions {
			if session.userID == a.db.UserID() && session.item == id && session.clientSession == request.PlaySessionId {
				previous = key
				break
			}
		}
		a.mediaMu.Unlock()
		if previous != "" {
			a.stopMediaSession(id, previous)
		}
	}
	release, ok := a.acquireMediaProcess()
	if !ok {
		c.JSON(429, gin.H{"error": "播放处理繁忙，请稍后重试"})
		return
	}
	dir, err := os.MkdirTemp("", "emby-go-playback-")
	if err != nil {
		release()
		c.Status(500)
		return
	}
	ctx, cancel := context.WithTimeout(a.rootCtx, 4*time.Hour)
	sessionID := randomSessionID()
	session := &audioSession{userID: a.db.UserID(), item: id, dir: dir, cancel: cancel, done: make(chan struct{})}
	session.clientSession = request.PlaySessionId
	session.touched.Store(time.Now().UnixNano())
	seekTime := strconv.FormatFloat(float64(request.StartTimeTicks)/1e7, 'f', 3, 64)
	args := []string{"-nostdin", "-v", "error", "-y", "-ss", seekTime,
		"-readrate", "1", "-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy", "-i", raw}
	audioMap := fmt.Sprintf("0:%d", containerIndex)
	if externalPath != "" {
		format := map[string]string{".mka": "matroska", ".flac": "flac", ".aac": "aac", ".ac3": "ac3", ".eac3": "eac3", ".dts": "dts", ".m4a": "mov", ".mp3": "mp3", ".ogg": "ogg", ".opus": "ogg", ".wav": "wav"}[strings.ToLower(filepath.Ext(externalPath))]
		args = append(args, "-ss", seekTime, "-protocol_whitelist", "file,crypto", "-f", format, "-i", externalPath)
		audioMap = "1:a:0"
	}
	args = append(args, "-map", "0:v:0", "-map", audioMap, "-c:v", "copy")
	if !external && strings.EqualFold(audioCodec, "aac") {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
	}
	args = append(args,
		"-f", "hls", "-hls_time", "4", "-hls_list_size", "0", "-hls_playlist_type", "event", "-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4",
		"-hls_segment_filename", filepath.Join(dir, "seg%06d.m4s"), filepath.Join(dir, "index.m3u8"))
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cacheLimit := int64(playbackCacheMB(a.configSnapshot())) << 20
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		os.RemoveAll(dir)
		release()
		c.JSON(502, gin.H{"error": "无法启动 FFmpeg"})
		return
	}
	a.mediaMu.Lock()
	if a.mediaSessions == nil {
		a.mediaSessions = map[string]*audioSession{}
	}
	a.mediaSessions[sessionID] = session
	// Register before releasing the process permit; Close waits for both stages.
	a.mediaSessionsWG.Add(1)
	a.mediaMu.Unlock()
	go func() {
		defer func() {
			defer a.mediaSessionsWG.Done()
			cancel()
			a.mediaMu.Lock()
			delete(a.mediaSessions, sessionID)
			a.mediaMu.Unlock()
			os.RemoveAll(dir)
			release()
			close(session.done)
		}()
		processDone := make(chan error, 1)
		go func() {
			err := cmd.Wait()
			release() // Completed processes do not occupy a slot while segments drain.
			processDone <- err
		}()
		processFinished := false
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if !processFinished {
					<-processDone
				}
				return
			case err := <-processDone:
				processFinished = true
				if err != nil {
					return
				}
			case <-ticker.C:
				if time.Since(time.Unix(0, session.touched.Load())) > 90*time.Second || sessionBytes(dir) > cacheLimit {
					cancel()
				}
			}
		}
	}()
	// Do not publish a session until a complete playlist and initial segment exist.
	deadline := time.NewTimer(a.configSnapshot().ProbeTimeout())
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			cancel()
			return
		case <-deadline.C:
			cancel()
			c.JSON(504, gin.H{"error": "音轨处理超时"})
			return
		case <-session.done:
			c.JSON(502, gin.H{"error": "音轨处理失败，可能是源地址或视频编码不支持重封装"})
			return
		case <-poll.C:
			if info, err := os.Stat(filepath.Join(dir, "index.m3u8")); err == nil && info.Size() > 0 {
				if c.GetBool("audioMaster") {
					c.Redirect(http.StatusFound, "/Videos/"+id+"/hls/"+sessionID+"/index.m3u8")
					return
				}
				c.JSON(200, gin.H{"SessionId": sessionID, "Url": "/Videos/" + id + "/hls/" + sessionID + "/index.m3u8", "StartTimeTicks": request.StartTimeTicks, "RunTimeTicks": movie.RuntimeSeconds * 10000000})
				return
			}
		}
	}
}

func (a *App) audioMaster(c *gin.Context) {
	if value, exists, err := streamSelection(c, "SubtitleStreamIndex"); exists && (err != nil || value >= 0) {
		c.JSON(415, gin.H{"error": "此播放地址仅处理音轨；文本字幕请单独加载，图形字幕烧录尚不支持"})
		return
	}
	index, exists, err := streamSelection(c, "AudioStreamIndex")
	if !exists || err != nil {
		c.Status(400)
		return
	}
	start, err := strconv.ParseInt(c.DefaultQuery("StartTimeTicks", "0"), 10, 64)
	if err != nil || start < 0 {
		c.Status(400)
		return
	}
	client := c.Query("PlaySessionId")
	if len(client) > 128 {
		c.Status(400)
		return
	}
	// Reuse the same validation and process owner as the administration player.
	body := fmt.Sprintf(`{"AudioStreamIndex":%d,"StartTimeTicks":%d,"PlaySessionId":%q}`, index, start, client)
	c.Request.Body = io.NopCloser(strings.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("audioMaster", true)
	a.startAudioPlayback(c)
}

func sessionBytes(dir string) int64 {
	entries, _ := os.ReadDir(dir)
	var total int64
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

func (a *App) stopMediaSession(item, id string) {
	a.mediaMu.Lock()
	session := a.mediaSessions[id]
	a.mediaMu.Unlock()
	if session != nil && session.item == item && session.userID == a.db.UserID() {
		session.cancel()
		<-session.done
	}
}

func (a *App) stopAudioPlayback(c *gin.Context) {
	a.mediaMu.Lock()
	session := a.mediaSessions[c.Param("session")]
	a.mediaMu.Unlock()
	if session == nil || session.userID != a.db.UserID() {
		c.Status(404)
		return
	}
	a.stopMediaSession(c.Param("id"), c.Param("session"))
	c.Status(204)
}

func (a *App) touchAudioPlayback(c *gin.Context) {
	a.mediaMu.Lock()
	session := a.mediaSessions[c.Param("session")]
	a.mediaMu.Unlock()
	if session == nil || session.item != c.Param("id") || session.userID != a.db.UserID() {
		c.Status(404)
		return
	}
	session.touched.Store(time.Now().UnixNano())
	c.Status(204)
}

var hlsFile = regexp.MustCompile(`^(index\.m3u8|init\.mp4|seg\d{6,}\.m4s)$`)

// Random 128-bit session IDs are scoped media capabilities. No account token is
// placed in the playlist or forwarded to the remote origin.
func (a *App) hlsResource(c *gin.Context) {
	if !hlsFile.MatchString(c.Param("file")) {
		c.Status(404)
		return
	}
	a.mediaMu.Lock()
	session := a.mediaSessions[c.Param("session")]
	a.mediaMu.Unlock()
	if session == nil || session.item != c.Param("id") {
		c.Status(404)
		return
	}
	user, err := a.db.User(session.userID)
	if err != nil || user.Disabled {
		c.Status(404)
		return
	}
	a = &App{appState: a.appState, db: a.db.ForUser(user.ID)}
	if !a.canAccessItem(session.item) {
		c.Status(404)
		return
	}
	session.touched.Store(time.Now().UnixNano())
	c.Header("X-Content-Type-Options", "nosniff")
	if strings.HasSuffix(c.Param("file"), ".m3u8") {
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
	}
	c.File(filepath.Join(session.dir, c.Param("file")))
}
