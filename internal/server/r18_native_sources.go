package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

// 列表只读取已有绑定；单片详情和播放信息才读取磁力，避免海报墙逐片请求资源。
func (a *App) r18NativeSources(c *gin.Context, l store.Library, v r18.Video, magnets bool) ([]gin.H, error) {
	matches, err := a.r18LocalMatches(c, v)
	if err != nil {
		return nil, err
	}
	local := a.r18LocalMediaSources(c, matches)
	ready := a.r18ReadySources(c, l, v.ID)
	resources := []gin.H{}
	if magnets {
		resources, err = a.r18MagnetSources(c, l, v)
		if err != nil && len(local) == 0 && len(ready) == 0 {
			return nil, err
		}
	}
	guangya := []gin.H{}
	legacy := []gin.H{}
	readyByHash := map[string]gin.H{}
	for _, source := range ready {
		if source["Provider"] != "guangya" {
			legacy = append(legacy, source)
			continue
		}
		hash, _ := source["InfoHash"].(string)
		id := r18MagnetSourceID(hash)
		if id != "" {
			// 任务重建后仍保持同一版本；已就绪文件提供真实容器、大小和时长。
			source["Id"] = id
			source["ResourceId"] = "btih-" + strings.ToLower(hash)
			source["Path"] = a.cloudStreamURL(l, v.ID, id)
			source["DirectStreamUrl"] = source["Path"]
			readyByHash[strings.ToLower(hash)] = source
		}
		guangya = append(guangya, source)
	}
	sources := []gin.H{}
	seenIDs, seenHashes := map[string]bool{}, map[string]bool{}
	appendSource := func(source gin.H) {
		id, _ := source["Id"].(string)
		hash, _ := source["InfoHash"].(string)
		hash = strings.ToLower(hash)
		if id == "" || seenIDs[id] || hash != "" && seenHashes[hash] {
			return
		}
		seenIDs[id] = true
		if hash != "" {
			seenHashes[hash] = true
		}
		source["Default"] = len(sources) == 0
		sources = append(sources, source)
	}
	for _, source := range resources {
		hash, _ := source["InfoHash"].(string)
		if cached := readyByHash[hash]; cached != nil {
			name := source["Name"]
			for key, value := range cached {
				source[key] = value
			}
			source["Name"] = name
		}
		appendSource(source)
	}
	// 默认光鸭，同一磁力的其它网盘绑定不重复显示为新版本。
	for _, group := range [][]gin.H{guangya, local, legacy} {
		for _, source := range group {
			appendSource(source)
		}
	}
	return sources, nil
}

func (a *App) r18NativePlaybackSources(c *gin.Context, l store.Library, v r18.Video, requested string) ([]gin.H, error) {
	if requested == "" {
		return a.r18NativeSources(c, l, v, true)
	}
	stored, err := a.r18NativeSources(c, l, v, false)
	if err != nil {
		return nil, err
	}
	for _, source := range stored {
		if source["Id"] == requested {
			source["Default"] = true
			return []gin.H{source}, nil
		}
	}
	// 网页已签发的任务 UUID 来源仍须精确可选，不能被版本去重或其它网盘替代。
	for _, source := range a.r18ReadySources(c, l, v.ID) {
		if source["Id"] == requested {
			source["Default"] = true
			return []gin.H{source}, nil
		}
	}
	if _, valid := parseR18MagnetSourceID(requested); valid {
		sources, err := a.r18NativeSources(c, l, v, true)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			if source["Id"] == requested {
				source["Default"] = true
				return []gin.H{source}, nil
			}
		}
	}
	return []gin.H{}, nil
}

// 原生客户端可能用 GET 查询或分块 POST 选择版本，两种入口遵循同一精确选择规则。
func r18RequestedSource(c *gin.Context, body bool) (string, bool) {
	requested, present := "", false
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "播放查询参数编码无效"})
		return "", false
	}
	for key, values := range query {
		if !strings.EqualFold(key, "MediaSourceId") {
			continue
		}
		for _, value := range values {
			if present && value != requested {
				c.JSON(http.StatusBadRequest, gin.H{"error": "播放来源参数冲突"})
				return "", false
			}
			requested, present = value, true
		}
	}
	if body && c.Request.Method == http.MethodPost && c.Request.Body != nil && c.Request.ContentLength != 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
		decoder := json.NewDecoder(c.Request.Body)
		selection, err := readR18SourceSelection(decoder)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的播放请求"})
			return "", false
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的播放请求"})
			return "", false
		}
		if selection != "" {
			if present && requested != "" && selection != requested {
				c.JSON(http.StatusBadRequest, gin.H{"error": "播放来源参数冲突"})
				return "", false
			}
			requested = selection
		}
	}
	if len(requested) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "播放来源参数过长"})
		return "", false
	}
	return requested, true
}

func readR18SourceSelection(decoder *json.Decoder) (string, error) {
	start, err := decoder.Token()
	if errors.Is(err, io.EOF) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if start != json.Delim('{') {
		return "", errors.New("播放请求必须是对象")
	}
	selection, present := "", false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return "", err
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return "", err
		}
		name, ok := key.(string)
		if !ok || !strings.EqualFold(name, "MediaSourceId") {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		// 同一对象里的重复键与大小写变体也不能将显式版本覆盖为空或另一版本。
		if present && selection != value {
			return "", errors.New("播放来源参数冲突")
		}
		selection, present = value, true
	}
	end, err := decoder.Token()
	if err != nil {
		return "", err
	}
	if end != json.Delim('}') {
		return "", errors.New("播放请求对象未结束")
	}
	return selection, nil
}
