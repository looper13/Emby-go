// Package anime contains online catalog contracts. Catalog identities never
// replace the original media ID which owns playback and user data.
package anime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Subject struct {
	ID           int64   `json:"id"`
	BangumiID    int64   `json:"bangumi_id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	Summary      string  `json:"summary"`
	Platform     string  `json:"platform"`
	Image        string  `json:"image"`
	Date         string  `json:"date"`
	Rating       float64 `json:"rating"`
	Episodes     int     `json:"episode_count"`
	UpdatedAt    string  `json:"updated_at"`
}

func (s Subject) IsMovie() bool {
	return s.Platform == "剧场版" || strings.EqualFold(s.Platform, "Movie")
}
func (s Subject) IsSeries() bool {
	switch strings.ToUpper(strings.TrimSpace(s.Platform)) {
	case "TV", "OVA", "ONA", "WEB", "TV动画":
		return true
	}
	return false
}
func (s Subject) ItemID() string {
	if s.IsMovie() {
		return ID("movie", s.ID)
	}
	return ID("series", s.ID)
}

type Episode struct {
	ID           int64       `json:"id"`
	SubjectID    int64       `json:"subject_id"`
	BangumiID    int64       `json:"bangumi_id"`
	Name         string      `json:"name"`
	OriginalName string      `json:"original_name"`
	Summary      string      `json:"summary"`
	Sort         json.Number `json:"sort"`
	Number       json.Number `json:"ep"`
	Active       bool        `json:"active"`
	Type         int         `json:"type"`
	Season       int         `json:"season"`
	Index        int         `json:"index"`
	AirDate      string      `json:"air_date"`
}

type Entry struct {
	ID        int64  `json:"id"`
	SubjectID int64  `json:"subject_id"`
	BangumiID int64  `json:"bangumi_id"`
	Name      string `json:"name"`
	Image     string `json:"image"`
	Year      int    `json:"year"`
	Season    int    `json:"season"`
	Weekday   int    `json:"weekday"`
	Manual    bool   `json:"manual"`
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updated_at"`
}
type Release struct {
	ID          string `json:"id"`
	MikanID     int64  `json:"mikan_id"`
	Title       string `json:"title"`
	Group       string `json:"group"`
	Size        int64  `json:"size"`
	SizeText    string `json:"size_text"`
	PublishedAt string `json:"published_at"`
	URL         string `json:"url"`
	Torrent     string `json:"torrent"`
	Magnet      string `json:"magnet"`
	Resolution  string `json:"resolution"`
	Subtitle    string `json:"subtitle"`
	EpisodeHint string `json:"episode_hint"`
	Ambiguous   bool   `json:"ambiguous"`
}
type Binding struct {
	ID            int64  `json:"id"`
	SubjectID     int64  `json:"subject_id"`
	EpisodeID     int64  `json:"episode_id"`
	MovieID       int64  `json:"movie_id"`
	Revision      int64  `json:"revision"`
	Status        string `json:"status"`
	SourceVersion string `json:"-"`
	CheckedAt     string `json:"checked_at"`
	Error         string `json:"error"`
}

func ID(kind string, id int64) string      { return "anime-" + kind + ":" + strconv.FormatInt(id, 10) }
func SeasonID(id int64, season int) string { return ID("season", id) + ":" + strconv.Itoa(season) }

// ParseID deliberately rejects partial, negative, zero, and noncanonical IDs.
// Unknown anime IDs must never fall through to a numeric media ID.
func ParseID(raw string) (kind string, id int64, season int, ok bool) {
	p := strings.Split(raw, ":")
	if len(p) < 2 || len(p) > 3 {
		return
	}
	kind = strings.TrimPrefix(p[0], "anime-")
	if p[0] != "anime-"+kind {
		return
	}
	if kind != "series" && kind != "season" && kind != "episode" && kind != "movie" {
		return
	}
	id, err := strconv.ParseInt(p[1], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != p[1] {
		return kind, 0, 0, false
	}
	if kind == "season" {
		if len(p) != 3 || (p[2] != "0" && p[2] != "1") {
			return kind, 0, 0, false
		}
		season, _ = strconv.Atoi(p[2])
	} else if len(p) != 2 {
		return kind, 0, 0, false
	}
	return kind, id, season, true
}

func ValidateEpisode(e Episode) error {
	n, err := e.Sort.Float64()
	if e.BangumiID <= 0 || err != nil || n < 0 || (e.Season != 0 && e.Season != 1) {
		return fmt.Errorf("invalid episode identity or sort")
	}
	return nil
}
