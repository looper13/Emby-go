package store

import (
	"database/sql"
	"strings"
)

// One set of statements serves every movie in a scan transaction. Other write
// paths use the same comparison and replacement rules for a single movie.
type featureStatements struct {
	tx                         *sql.Tx
	selectValues, deleteValues *sql.Stmt
	inserts                    map[int]*sql.Stmt
}

func prepareFeatureStatements(tx *sql.Tx) (*featureStatements, error) {
	w := &featureStatements{tx: tx, inserts: make(map[int]*sql.Stmt)}
	for _, statement := range []struct {
		target **sql.Stmt
		query  string
	}{
		{&w.selectValues, "SELECT kind,value,weight FROM movie_features WHERE movie_id=?"},
		{&w.deleteValues, "DELETE FROM movie_features WHERE movie_id=?"},
	} {
		var err error
		*statement.target, err = tx.Prepare(statement.query)
		if err != nil {
			w.close()
			return nil, err
		}
	}
	return w, nil
}

func (w *featureStatements) close() {
	for _, statement := range []*sql.Stmt{w.selectValues, w.deleteValues} {
		if statement != nil {
			statement.Close()
		}
	}
	for _, statement := range w.inserts {
		statement.Close()
	}
}

func (w *featureStatements) insert(args []any) error {
	count := len(args) / 4
	statement := w.inserts[count]
	if statement == nil {
		var err error
		statement, err = w.tx.Prepare("INSERT OR REPLACE INTO movie_features(movie_id,kind,value,weight) VALUES" + strings.TrimSuffix(strings.Repeat("(?,?,?,?),", count), ","))
		if err != nil {
			return err
		}
		w.inserts[count] = statement
	}
	_, err := statement.Exec(args...)
	return err
}

func saveFeatureValuesTx(tx *sql.Tx, movieID int64, next map[string]int) error {
	w, err := prepareFeatureStatements(tx)
	if err != nil {
		return err
	}
	defer w.close()
	return w.save(movieID, next)
}

func (w *featureStatements) save(movieID int64, next map[string]int) error {
	existing := make(map[string]int, len(next))
	rows, err := w.selectValues.Query(movieID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var kind, value string
		var weight int
		if err = rows.Scan(&kind, &value, &weight); err != nil {
			rows.Close()
			return err
		}
		existing[kind+"\x00"+value] = weight
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(existing) == len(next) {
		same := true
		for key, weight := range next {
			if existing[key] != weight {
				same = false
				break
			}
		}
		if same {
			return nil
		}
	}
	if _, err = w.deleteValues.Exec(movieID); err != nil {
		return err
	}
	// Batching also avoids per-feature SQLite compilation in drivers that only
	// retain the SQL text when database/sql.Prepare is called.
	const batchSize = 128
	args := make([]any, 0, 4*min(batchSize, len(next)))
	for key, weight := range next {
		kind, value, _ := strings.Cut(key, "\x00")
		args = append(args, movieID, kind, value, weight)
		if len(args) == 4*batchSize {
			if err := w.insert(args); err != nil {
				return err
			}
			args = args[:0]
		}
	}
	if len(args) > 0 {
		return w.insert(args)
	}
	return nil
}
