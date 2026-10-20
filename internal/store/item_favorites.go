package store

import "strconv"

// Playable media keep their existing userdata rows. Named entities and virtual
// folders use stable item IDs and never inherit a child's favorite state.
func (s *Store) loadItemFavorites() error {
	rows, err := s.db.Query("SELECT item_id FROM item_favorites")
	if err != nil {
		return err
	}
	defer rows.Close()
	s.itemFavorites = map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		s.itemFavorites[id] = true
	}
	return rows.Err()
}

func (s *Store) ItemFavorite(id string) bool {
	s.itemFavoritesMu.RLock()
	defer s.itemFavoritesMu.RUnlock()
	return s.itemFavorites[id]
}

func (s *Store) ItemFavoriteIDs() []string {
	s.itemFavoritesMu.RLock()
	defer s.itemFavoritesMu.RUnlock()
	ids := make([]string, 0, len(s.itemFavorites))
	for id := range s.itemFavorites {
		ids = append(ids, id)
	}
	return ids
}

func (s *Store) ItemFavoriteVersion() string {
	return s.movieEpoch + ":" + strconv.FormatUint(s.itemFavoriteVersion.Load(), 10)
}

func (s *Store) SetItemFavorite(id string, favorite bool) error {
	s.itemFavoritesMu.Lock()
	defer s.itemFavoritesMu.Unlock()
	var err error
	if favorite {
		_, err = s.db.Exec("INSERT INTO item_favorites(item_id) VALUES(?) ON CONFLICT DO NOTHING", id)
	} else {
		_, err = s.db.Exec("DELETE FROM item_favorites WHERE item_id=?", id)
	}
	if err != nil {
		return err
	}
	if favorite {
		s.itemFavorites[id] = true
	} else {
		delete(s.itemFavorites, id)
	}
	s.itemFavoriteVersion.Add(1)
	return nil
}
