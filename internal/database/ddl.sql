CREATE TABLE IF NOT EXISTS document (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	identifier TEXT    NOT NULL UNIQUE,
	content    TEXT    NOT NULL,
	UNIQUE (identifier)
);

CREATE TABLE IF NOT EXISTS document_token (
	document_id INTEGER NOT NULL,
	position    INTEGER NOT NULL,
	token       TEXT    NOT NULL,
	UNIQUE (document_id, position),
	FOREIGN KEY (document_id) REFERENCES document (id) ON DELETE CASCADE
);
