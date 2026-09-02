package typosearch

import (
	"github.com/RadhiFadlillah/typo-search/internal/database"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// Storage is the container for storing trigram indexes for documents that will be
// searched later. Use sqlite3 as database engine.
type Storage struct {
	db *sqlx.DB
}

// OpenStorage open the trigram indexes in the specified path.
func OpenStorage(path string) (*Storage, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}

	return &Storage{db}, nil
}
