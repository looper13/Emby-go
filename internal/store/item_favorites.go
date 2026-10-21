package store

import (
	"strconv"
	"strings"
)

// Playable media keep their existing userdata rows. Named entities and virtual
// folders use stable item IDs and never inherit a child's favorite state.
func (s *Store) loadItemFavorites() error {
	rows, err := s.db.Query("SELECT user_id,item_id FROM item_favorites")
	if err != nil {
		return err
	}
	defer rows.Close()
	s.itemFavorites = map[string]bool{}
	for rows.Next() {
		var id string
		var uid int64
		if err := rows.Scan(&uid, &id); err != nil {
			return err
		}
		s.itemFavorites[strconv.FormatInt(uid, 10)+":"+id] = true
	}
	return rows.Err()
}

func (s *Store) ItemFavorite(id string) bool {
	s.itemFavoritesMu.RLock()
	defer s.itemFavoritesMu.RUnlock()
	return s.itemFavorites[s.userKey()+":"+id]
}

func (s *Store) ItemFavoriteIDs() []string {
	s.itemFavoritesMu.RLock()
	defer s.itemFavoritesMu.RUnlock()
	ids := make([]string, 0, len(s.itemFavorites))
	for id := range s.itemFavorites {
		if value, ok := strings.CutPrefix(id, s.userKey()+":"); ok {
			ids = append(ids, value)
		}
	}
	return ids
}

func (s *Store) ItemFavoriteVersion() string {
	return s.movieEpoch + ":user:" + s.userKey() + ":" + strconv.FormatUint(s.itemFavoriteVersion.Load(), 10)
}

func (s *Store) SetItemFavorite(id string, favorite bool) error {
	s.itemFavoritesMu.Lock()
	defer s.itemFavoritesMu.Unlock()
	var err error
	if favorite {
		_, err = s.db.Exec("INSERT INTO item_favorites(user_id,item_id) VALUES(?,?) ON CONFLICT DO NOTHING", s.UserID(), id)
	} else {
		_, err = s.db.Exec("DELETE FROM item_favorites WHERE user_id=? AND item_id=?", s.UserID(), id)
	}
	if err != nil {
		return err
	}
	if favorite {
		s.itemFavorites[s.userKey()+":"+id] = true
	} else {
		delete(s.itemFavorites, s.userKey()+":"+id)
	}
	s.itemFavoriteVersion.Add(1)
	return nil
}
