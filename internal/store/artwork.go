package store

// Backdrops retains single-image records until the next scan upgrades them.
func (m Movie) Backdrops() []string {
	if len(m.BackdropPaths) > 0 {
		return m.BackdropPaths
	}
	if m.BackdropPath != "" {
		return []string{m.BackdropPath}
	}
	return []string{}
}
