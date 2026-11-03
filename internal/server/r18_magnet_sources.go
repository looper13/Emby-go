package server

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"emby-go/internal/clouddrive"
	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

const r18MagnetSourcePrefix = "guangya-magnet-"

// 磁力版本使用规范哈希作为身份，离线任务重试、过期和重建都不改变版本 ID。
func r18MagnetSourceID(hash string) string {
	source := r18MagnetSourcePrefix + strings.ToLower(strings.TrimSpace(hash))
	if _, ok := parseR18MagnetSourceID(source); !ok {
		return ""
	}
	return source
}

func parseR18MagnetSourceID(source string) (string, bool) {
	if !strings.HasPrefix(source, r18MagnetSourcePrefix) {
		return "", false
	}
	hash := strings.TrimPrefix(source, r18MagnetSourcePrefix)
	if len(hash) != 40 {
		return "", false
	}
	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return "", false
		}
	}
	return hash, true
}

// 完整读取是可选能力，旧测试替身和其他来源仍可使用原有分页接口。
type r18AllResourcesSource interface {
	AllResources(context.Context, r18.Video) ([]r18.Resource, error)
}

func readR18MagnetResources(ctx context.Context, client r18Source, video r18.Video) ([]r18.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if complete, ok := client.(r18AllResourcesSource); ok {
		return complete.AllResources(ctx, video)
	}
	resources := []r18.Resource{}
	for offset := 0; ; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := client.Resources(ctx, video, offset)
		if err != nil {
			return nil, err
		}
		resources = append(resources, page.Items...)
		if !page.HasMore {
			return resources, nil
		}
		// 禁止空页、倒退和无界分页使详情请求反复读取上游。
		if len(page.Items) == 0 || page.NextOffset <= offset || page.NextOffset > 10000 {
			return nil, errors.New("JavDB 磁力分页位置无效或超过范围")
		}
		offset = page.NextOffset
	}
}

func (a *App) r18MagnetSources(c *gin.Context, l store.Library, v r18.Video) ([]gin.H, error) {
	sources := []gin.H{}
	if _, err := a.db.CloudGuangyaConnection(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sources, nil
		}
		return sources, err
	}
	client, err := a.r18Client(l)
	if err != nil {
		return sources, err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	resources, err := readR18MagnetResources(ctx, client, v)
	if err != nil {
		return sources, err
	}
	seen := map[string]bool{}
	numberReplacer := strings.NewReplacer("-", "", "_", "", " ", "")
	normalizedNumber := func(number string) string {
		return strings.ToUpper(numberReplacer.Replace(strings.TrimSpace(number)))
	}
	for _, resource := range resources {
		if resource.Number != "" && normalizedNumber(resource.Number) != normalizedNumber(v.Number) {
			continue
		}
		// 展示资源允许 BTv2，光鸭离线播放只接受无冲突、归属一致的 BTIH。
		magnet := r18.SafeMagnet(resource.Magnet)
		_, hash, err := clouddrive.NormalizeMagnet(magnet)
		if err != nil || resource.ID != "btih-"+hash || seen[hash] {
			continue
		}
		id := r18MagnetSourceID(hash)
		if id == "" {
			continue
		}
		seen[hash] = true
		name := strings.TrimSpace(resource.Title)
		if name == "" {
			name = strings.TrimSpace(v.Number)
		}
		if name == "" {
			name = "磁力版本"
		}
		stream := a.cloudStreamURL(l, v.ID, id)
		source := gin.H{
			"Id":                         id,
			"Name":                       "光鸭 · " + name + " · " + hash[:8],
			"Type":                       "Default",
			"ItemId":                     r18ItemID(l.ID, v.ID),
			"Protocol":                   "Http",
			"Container":                  "",
			"Path":                       stream,
			"DirectStreamUrl":            stream,
			"IsRemote":                   true,
			"HasMixedProtocols":          false,
			"SupportsDirectPlay":         true,
			"SupportsDirectStream":       true,
			"SupportsTranscoding":        false,
			"SupportsProbing":            false,
			"RequiresOpening":            false,
			"RequiresClosing":            false,
			"RequiresLooping":            false,
			"IsInfiniteStream":           false,
			"AddApiKeyToDirectStreamUrl": false,
			"MediaStreams":               genericVideoStream(),
			"DefaultAudioStreamIndex":    -1,
			"DefaultSubtitleStreamIndex": -1,
			"Chapters":                   []gin.H{},
			"Formats":                    []string{},
			"RequiredHttpHeaders":        gin.H{},
			"ReadAtNativeFramerate":      false,
			"Default":                    len(sources) == 0,
			"InfoHash":                   hash,
			"ResourceId":                 resource.ID,
			"Provider":                   "guangya",
		}
		if resource.SizeBytes > 0 {
			source["Size"] = resource.SizeBytes
		}
		if v.Duration > 0 && int64(v.Duration) <= math.MaxInt64/600000000 {
			source["RunTimeTicks"] = int64(v.Duration) * 600000000
		}
		// 这里只签发本服务播放入口，任务创建和文件核验在真正点播时进行。
		sources = append(sources, source)
	}
	return sources, nil
}
