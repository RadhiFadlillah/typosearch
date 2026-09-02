package typosearch

import (
	"github.com/RadhiFadlillah/typosearch/internal/database"
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

// Close closes the underlying database for the search storage.
func (s *Storage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Apply one or more [Processor] function to the [Storage]. These processors later
// will be used on the submitted [Document] and on search queries. These processors
// are not saved inside Storage, so make sure to re-apply it whenever you open the
// storage.
func (s *Storage) ApplyProcessors(processors ...Processor) {
	s.processors = processors
}

// AddDocuments save and index the documents into the storage. If the document with
// matching ID already exist, it will be updated.
func (s *Storage) AddDocuments(docs ...Document) error {
	// Cast Document to insert arg
	dbDocs := make([]database.InsertDocumentArg, len(docs))
	for i, doc := range docs {
		dbDocs[i] = database.InsertDocumentArg{
			Identifier: doc.ID,
			Content:    doc.Content,
		}
	}

	// Cast Processors into normal function
	dbProcessors := make([]func(rune) []rune, len(s.processors))
	for i, p := range s.processors {
		dbProcessors[i] = p
	}

	return database.InsertDocuments(s.db, dbProcessors, dbDocs)
}

// DeleteDocuments remove the documents in the storage.
func (st *Storage) DeleteDocuments(ids ...string) error {
	return database.DeleteDocuments(st.db, ids...)
}
