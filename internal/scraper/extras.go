package scraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emby-go/internal/imageutil"
	"emby-go/internal/mediaassets"
	"emby-go/internal/metatube"
	"emby-go/internal/store"
)

const maxTrailerBytes = 512 << 20

func trailerURL(info metatube.MovieInfo) string {
	if value := strings.TrimSpace(info.PreviewVideoURL); value != "" {
		return value
	}
	return strings.TrimSpace(info.PreviewVideoHLSURL)
}

func (s *Scraper) downloadExtras(ctx context.Context, movie store.Movie, info metatube.MovieInfo, dir string, overwrite bool, result *ApplyResult) error {
	warn := func(kind string, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		message := kind + ": " + err.Error()
		result.ImageWarn = append(result.ImageWarn, message)
		slog.Warn("刮削下载附加资源失败", "movie_id", movie.ID, "kind", kind, "error", err)
		return nil
	}
	if s.cfg.DownloadStills {
		for index, raw := range info.PreviewImages {
			if err := ctx.Err(); err != nil {
				return err
			}
			dest := imageutil.MovieImageDestination(movie.SourcePath, dir, fmt.Sprintf("fanart%d", index+1))
			if !overwrite && nonemptyFile(dest) {
				continue
			}
			data, err := s.client.Download(ctx, raw, maxImageBytes)
			var path string
			if err == nil {
				path, err = saveImage(ctx, data, dest, overwrite)
			}
			if err != nil {
				if err := warn(fmt.Sprintf("剧照 %d", index+1), err); err != nil {
					return err
				}
				continue
			}
			if path != "" {
				result.Images = append(result.Images, path)
				result.ImageCount++
			}
		}
		if len(info.PreviewImages) == 0 {
			if err := warn("剧照", errors.New("该来源未提供剧照")); err != nil {
				return err
			}
		}
	}
	if s.cfg.DownloadTrailer {
		dest := mediaassets.TrailerPath(movie.SourcePath, dir)
		if !overwrite && nonemptyFile(dest) {
			return nil
		}
		raw := strings.TrimSpace(info.PreviewVideoURL)
		if raw == "" {
			return warn("预告片", errors.New("该来源未提供可下载的 MP4 预告片；HLS 地址仍可在线播放"))
		}
		path, err := s.downloadTrailer(ctx, raw, dest, overwrite)
		if err != nil {
			return warn("预告片", err)
		}
		result.TrailerPath = path
	}
	return nil
}

func nonemptyFile(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.Mode().IsRegular() && stat.Size() > 0
}

// Stream into a same-directory temporary file. Failed/cancelled downloads never replace a trailer.
func (s *Scraper) downloadTrailer(ctx context.Context, raw, dest string, overwrite bool) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+"-*.tmp")
	if err != nil {
		return "", err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	_, downloadErr := s.client.DownloadTo(ctx, raw, file, maxTrailerBytes, max(10*time.Minute, s.cfg.Timeout()))
	closeErr := file.Close()
	if err := errors.Join(downloadErr, closeErr); err != nil {
		return "", err
	}
	// Reject error pages and HLS playlists returned with a misleading 200 status.
	check, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	var header [12]byte
	_, err = io.ReadFull(check, header[:])
	check.Close()
	if err != nil || string(header[4:8]) != "ftyp" {
		return "", errors.New("预告片响应不是 MP4 文件")
	}
	return commitAsset(ctx, tmp, dest, overwrite)
}
