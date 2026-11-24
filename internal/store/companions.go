package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Companion is a registered companion service (docs/spec/companions.md).
type Companion struct {
	Name    string
	Title   string
	URL     string
	Token   string // Mira passes its page's calls on with it
	TokenID string // the access token that registered it
	// SkillsPage is the path of its skills page under its page; empty for
	// none (docs/spec/companions.md, decision 29).
	SkillsPage string
	Updated    time.Time
}

// PutCompanion keeps a registration; the newest of a name wins, whichever token made it (decision 6). takenFrom is
// the token id of a registration it replaced that another token had made; empty otherwise.
func (s *Store) PutCompanion(ctx context.Context, c Companion) (takenFrom string, err error) {
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var owner string
		err := tx.QueryRowContext(ctx, `SELECT token_id FROM companions WHERE name = ?`, c.Name).Scan(&owner)
		switch {
		case err == nil && owner != c.TokenID:
			takenFrom = owner
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO companions(name, title, url, token, token_id, skills_page, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET title = excluded.title, url = excluded.url, token = excluded.token,
    token_id = excluded.token_id, skills_page = excluded.skills_page, updated_at = excluded.updated_at`,
			c.Name, c.Title, c.URL, c.Token, c.TokenID, c.SkillsPage, s.now().Format(timeFormat))
		return err
	})
	return takenFrom, err
}

// DeleteCompanion removes a registration of that token; false when there was none of it.
func (s *Store) DeleteCompanion(ctx context.Context, name, tokenID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM companions WHERE name = ? AND token_id = ?`, name, tokenID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Companions are the registered companions, by name.
func (s *Store) Companions(ctx context.Context) ([]Companion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, title, url, token, token_id, skills_page, updated_at FROM companions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Companion
	for rows.Next() {
		var c Companion
		var at string
		if err := rows.Scan(&c.Name, &c.Title, &c.URL, &c.Token, &c.TokenID, &c.SkillsPage, &at); err != nil {
			return nil, err
		}
		c.Updated, _ = time.Parse(timeFormat, at)
		out = append(out, c)
	}
	return out, rows.Err()
}
