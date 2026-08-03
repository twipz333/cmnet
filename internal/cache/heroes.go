package cache

// Hero is a Dota hero as cached locally. Ids are Valve hero ids and are NOT
// contiguous; the model's hero index map lives in the bundle, not here.
type Hero struct {
	ID        int
	Name      string // internal name, e.g. "npc_dota_hero_antimage"
	Localized string // display name, e.g. "Anti-Mage"
}

// UpsertHeroes inserts or updates the hero reference list.
func (s *Store) UpsertHeroes(heroes []Hero) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO heroes(id, name, localized) VALUES(?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name = excluded.name, localized = excluded.localized`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, h := range heroes {
		if _, err := stmt.Exec(h.ID, h.Name, h.Localized); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Heroes returns all cached heroes ordered by id (deterministic).
func (s *Store) Heroes() ([]Hero, error) {
	rows, err := s.db.Query("SELECT id, name, localized FROM heroes ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Hero
	for rows.Next() {
		var h Hero
		if err := rows.Scan(&h.ID, &h.Name, &h.Localized); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
