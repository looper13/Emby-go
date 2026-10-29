package store

import (
	"context"
	"errors"
	"strings"
)

const r18LocalNumberLimit = 1000
const r18LocalMovieLimit = 2000

// LocalSTRMMovies 批量匹配用户可访问目录库中的精确番号，不读取 STRM 文件或远程地址。
func (s *Store) LocalSTRMMovies(ctx context.Context, numbers []string) ([]Movie, error) {
	items := []Movie{}
	if len(numbers) > r18LocalNumberLimit {
		return nil, errors.New("本地 STRM 匹配一次最多支持 1000 个番号")
	}
	if len(numbers) == 0 {
		return items, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := []any{SourceDirectory}
	seen := make(map[string]bool, len(numbers))
	for _, number := range numbers {
		key := NormalizeDuplicateNumber(number)
		if key != "" && !seen[key] {
			seen[key] = true
			args = append(args, key)
		}
	}
	if len(seen) == 0 {
		return items, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(seen)), ",")
	// 多取一条仅用于检测超限，不能静默遗漏同一影片的其它本地版本。
	query := "SELECT " + movieCols + ` FROM movies WHERE status IN ('success','manual') AND television='null'
		AND library_id IN (SELECT id FROM ` + s.userLibrariesSQL() + ` WHERE source_type=?)
		AND ` + duplicateNumberSQL + ` IN (` + placeholders + `) ORDER BY library_id,source_path,id LIMIT ?`
	args = append(args, r18LocalMovieLimit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		movie, err := movieScan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, movie)
		if len(items) > r18LocalMovieLimit {
			return nil, errors.New("本地 STRM 匹配超过 2000 个候选，请缩小查询范围")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
