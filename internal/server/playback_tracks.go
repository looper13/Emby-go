package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/scanner"
	"emby-go/internal/store"
	"emby-go/internal/subtitle"
	"github.com/gin-gonic/gin"
)

type trackSnapshot struct {
	files          []subtitle.File
	audios         []subtitle.File
	tag            string
	streams        []gin.H
	size, duration int64
	probed         bool
}

// enableDetailTracks is called only by single-item detail and playback handlers.
// List DTOs deliberately share neither this snapshot nor its filesystem work.
func (a *App) enableDetailTracks(c *gin.Context, movie store.Movie, path string) {
	if !movie.IsVisible() || path == "" {
		return
	}
	if _, exists := c.Get("detailTracks:" + path); exists {
		return
	}
	discover := a.discoverSubtitles
	if discover == nil {
		discover = subtitle.Discover
	}
	files := discover(path)
	audios := subtitle.DiscoverAudio(path)
	hash := sha256.New()
	raw, _ := scanner.ReadSource(path)
	fmt.Fprintf(hash, "%s %t %x\n", raw, a.configSnapshot().FFmpegEnabled, a.subtitleSigningKey)
	snapshot := trackSnapshot{files: files, audios: audios}
	if info, err := os.Stat(movie.NFOPath); err == nil {
		fmt.Fprintln(hash, info.Size(), info.ModTime().UnixNano())
	}
	if file, ok := loadMediaInfo(path, raw); ok {
		if info, err := infoFromMediaInfo(file); err == nil {
			snapshot.streams, snapshot.size, snapshot.duration, snapshot.probed = buildStreams(streamDetailsFromProbe(info)), info.SizeBytes, info.DurationSeconds, true
			hash.Write(file.FFProbe)
		}
	}
	for _, file := range append(append([]subtitle.File{}, files...), audios...) {
		fmt.Fprintln(hash, file.Name)
		if info, err := os.Stat(file.Path); err == nil {
			fmt.Fprintln(hash, info.Size(), info.ModTime().UnixNano())
		}
	}
	snapshot.tag = fmt.Sprintf("%x", hash.Sum(nil))
	c.Set("detailTracks:"+path, snapshot)
}

func (a *App) detailSource(source gin.H, movie store.Movie, id, path string, c *gin.Context) gin.H {
	value, enabled := c.Get("detailTracks:" + path)
	if !enabled {
		return source
	}
	snapshot := value.(trackSnapshot)
	if snapshot.size > 0 {
		source["Size"] = snapshot.size
	}
	if snapshot.duration > 0 {
		source["RunTimeTicks"] = snapshot.duration * 10000000
	}
	// Cached NFO maps are immutable; decorate private copies for this response.
	tracks := a.resolveDetailTracks(movie, path, snapshot)
	streams := make([]gin.H, 0, len(tracks))
	for _, track := range tracks {
		stream := track.stream
		if stream["Type"] == "Subtitle" && stream["IsExternal"] != true && stream["ContainerIndexKnown"] == true && a.configSnapshot().FFmpegEnabled {
			index, _ := stream["Index"].(int)
			if stream["IsTextSubtitleStream"] == true {
				stream["SupportsExternalStream"] = true
				stream["DeliveryMethod"], stream["DeliveryFormat"] = "External", "vtt"
				stream["DeliveryUrl"] = a.subtitleURL(id, source["Id"].(string), index, "vtt", snapshot.tag)
			}
		}
		if stream["Type"] == "Subtitle" && track.file != nil {
			stream["DeliveryUrl"] = a.subtitleURL(id, source["Id"].(string), stream["Index"].(int), track.file.Codec, snapshot.tag)
		}
		streams = append(streams, stream)
	}
	source["MediaStreams"] = streams
	source["DefaultAudioStreamIndex"] = -1
	for _, stream := range streams {
		if stream["Type"] == "Audio" && stream["IsExternal"] != true && (source["DefaultAudioStreamIndex"] == -1 || stream["IsDefault"] == true) {
			source["DefaultAudioStreamIndex"] = stream["Index"]
			if stream["IsDefault"] == true {
				break
			}
		}
	}
	source["DefaultSubtitleStreamIndex"] = -1
	for _, stream := range streams {
		if stream["Type"] == "Subtitle" && stream["IsDefault"] == true {
			if source["DefaultSubtitleStreamIndex"] == -1 || stream["IsExternal"] == true {
				source["DefaultSubtitleStreamIndex"] = stream["Index"]
			}
			if stream["IsExternal"] == true {
				break
			}
		}
	}
	source["SupportsAudioSelection"] = a.configSnapshot().FFmpegEnabled
	// Public offsets may shift when files or metadata change; discard saved choices.
	source["TrackRevision"] = snapshot.tag
	return source
}

type resolvedTrack struct {
	stream gin.H
	file   *subtitle.File
}

// One ordering for metadata, subtitle delivery and audio processing.
func (a *App) resolveDetailTracks(movie store.Movie, path string, snapshot trackSnapshot) []resolvedTrack {
	streams := snapshot.streams
	if streams == nil {
		raw, _ := scanner.ReadSource(path)
		streams = a.currentPlaybackStreams(movie, path, raw)
	}
	tracks := make([]resolvedTrack, 0, len(streams)+len(snapshot.audios)+len(snapshot.files))
	for _, stream := range cloneStreams(streams) {
		stream["Index"] = len(tracks)
		tracks = append(tracks, resolvedTrack{stream: stream})
	}
	for _, file := range snapshot.audios {
		tracks = append(tracks, resolvedTrack{file: &file, stream: gin.H{"Type": "Audio", "Index": len(tracks), "Language": file.Language,
			"DisplayTitle": firstNonEmpty(file.Title, displayLanguage(file.Language), file.Name), "IsExternal": true,
			"IsDefault": file.Default, "IsForced": false, "IsTextSubtitleStream": false, "SupportsExternalStream": false,
			"ContainerIndexKnown": true, "Protocol": "File"}})
	}
	for _, file := range snapshot.files {
		format := file.Codec
		title := firstNonEmpty(file.Title, displayLanguage(file.Language), file.Language, file.Name)
		tracks = append(tracks, resolvedTrack{file: &file, stream: gin.H{
			"Type": "Subtitle", "Index": len(tracks), "Codec": file.Codec, "Language": file.Language,
			"Title": file.Title, "DisplayTitle": title, "DisplayLanguage": displayLanguage(file.Language),
			"IsExternal": true, "IsDefault": file.Default, "IsForced": file.Forced,
			"IsHearingImpaired": file.HearingImpaired, "IsTextSubtitleStream": subtitle.IsText(file.Codec),
			"SupportsExternalStream": true, "Protocol": "File", "DeliveryMethod": "External",
			"DeliveryFormat": format,
		}})
	}
	return tracks
}

func cloneStreams(streams []gin.H) []gin.H {
	out := make([]gin.H, len(streams))
	for i, stream := range streams {
		out[i] = gin.H{}
		for key, value := range stream {
			out[i][key] = value
		}
	}
	return out
}

// Never apply indexes from a different segment or a replaced remote source.
// Saved raw JSON can upgrade the projection without another network probe.
func (a *App) currentPlaybackStreams(movie store.Movie, path, raw string) []gin.H {
	if file, ok := loadMediaInfo(path, raw); ok {
		if info, err := infoFromMediaInfo(file); err == nil {
			return buildStreams(streamDetailsFromProbe(info))
		}
	}
	if state := nfoProbeState(movie.NFOPath); state.URL != "" && state.URL != raw {
		return genericVideoStream()
	}
	streams, _ := a.streamsFor(movie, path)
	if path != movie.SourcePath {
		streams = cloneStreams(streams)
		for _, stream := range streams {
			stream["ContainerIndexKnown"] = false
			delete(stream, "ContainerIndex")
		}
	}
	return streams
}

func (a *App) subtitleSignature(id, source, index, revision, expires string) []byte {
	hash := hmac.New(sha256.New, a.subtitleSigningKey[:])
	fmt.Fprintf(hash, "%s\n%s\n%s\n%s\n%s", id, source, index, revision, expires)
	return hash.Sum(nil)
}

func (a *App) subtitleURL(id, source string, index int, format, revision string) string {
	expires := strconv.FormatInt(time.Now().Add(4*time.Hour).Unix(), 10)
	query := url.Values{"revision": {revision}, "expires": {expires}, "SubtitleToken": {hex.EncodeToString(a.subtitleSignature(id, source, strconv.Itoa(index), revision, expires))}}
	return "/Videos/" + url.PathEscape(id) + "/" + url.PathEscape(source) + "/Subtitles/" + strconv.Itoa(index) + "/Stream." + format + "?" + query.Encode()
}

func (a *App) validSubtitleSignature(c *gin.Context) bool {
	expires, err := strconv.ParseInt(c.Query("expires"), 10, 64)
	if err != nil || expires <= time.Now().Unix() {
		return false
	}
	provided, err := hex.DecodeString(c.Query("SubtitleToken"))
	return err == nil && hmac.Equal(provided, a.subtitleSignature(c.Param("id"), c.Param("source"), c.Param("index"), c.Query("revision"), c.Query("expires")))
}

func (a *App) subtitleStream(c *gin.Context) {
	authenticated := a.authOK(c)
	if !authenticated && !a.validSubtitleSignature(c) {
		c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
		return
	}
	if ticks := c.Param("ticks"); ticks != "" {
		if value, err := strconv.ParseInt(ticks, 10, 64); err != nil || value < 0 {
			c.Status(400)
			return
		}
	}
	movie, path, id, ok := a.resolvePlaybackTarget(c.Param("id"))
	if !ok || !movie.IsVisible() || c.Param("source") != "mediasource_"+id {
		c.Status(404)
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		c.Status(400)
		return
	}
	format := subtitle.NormalizeFormat(c.Param("format"))
	if format != "srt" && format != "vtt" && format != "ass" && format != "ssa" && format != "sup" && format != "smi" {
		c.Status(400)
		return
	}
	a.enableDetailTracks(c, movie, path)
	snapshot, _ := c.Get("detailTracks:" + path)
	if snapshot == nil {
		c.Status(404)
		return
	}
	if !authenticated && c.Query("revision") != snapshot.(trackSnapshot).tag {
		c.Status(404)
		return
	}
	tracks := a.resolveDetailTracks(movie, path, snapshot.(trackSnapshot))
	for _, track := range tracks {
		stream := track.stream
		if stream["Index"] != index || stream["Type"] != "Subtitle" {
			continue
		}
		if track.file == nil {
			if stream["IsExternal"] != true && stream["IsTextSubtitleStream"] == true && stream["ContainerIndexKnown"] == true {
				raw, err := scanner.ReadSource(path)
				if err != nil || !scanner.ValidHTTP(raw) {
					c.Status(404)
					return
				}
				// Keep full source timestamps for both URL variants, including seeks.
				a.extractSubtitle(c, raw, stream["ContainerIndex"].(int), format)
				return
			}
			break
		}
		file := track.file
		data, err := readSubtitleFile(path, file.Path)
		if err != nil {
			c.JSON(404, gin.H{"error": "字幕文件不可用"})
			return
		}
		if !subtitle.IsText(file.Codec) {
			if format != file.Codec {
				c.JSON(415, gin.H{"error": "图形字幕不支持文本转换"})
				return
			}
		} else {
			data, err = subtitle.Convert(data, file.Codec, format)
			if err != nil {
				c.JSON(415, gin.H{"error": err.Error()})
				return
			}
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(200, subtitle.MIME(format), data)
		return
	}
	c.Status(404)
}

// Resolve symlinks as well as names, then limit bytes before conversion.
func readSubtitleFile(mediaPath, path string) ([]byte, error) {
	return subtitle.ReadLocal(mediaPath, path)
}

func streamSelection(c *gin.Context, key string) (int, bool, error) {
	for name, values := range c.Request.URL.Query() {
		if strings.EqualFold(name, key) && len(values) > 0 {
			value, err := strconv.Atoi(values[0])
			return value, true, err
		}
	}
	return -1, false, nil
}
