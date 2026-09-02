package typosearch

import (
	"github.com/RadhiFadlillah/typo-search/internal/database"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// Document is the text document that will be indexed to be later searched on.
type Document struct {
	// ID is the unique identifier for this Document.
	ID string
	// Content is the text body of this Document.
	Content string
}

// Storage is the container for storing trigram indexes for documents that will be
// searched later. Use sqlite3 as database engine.
type Storage struct {
	db         *sqlx.DB
	processors []Processor
}

// Open the search storage in the specified path.
func OpenStorage(path string) (*Storage, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}

	return &Storage{db: db}, nil
}

// Apply one or more [Processor] function to the [Storage]. These processors later
// will be used on the submitted [Document] and on search queries. These processors
// are not saved inside Storage, so make sure to re-apply it whenever you open the
// storage.
func (s *Storage) ApplyProcessors(processors ...Processor) {
	s.processors = processors
}
