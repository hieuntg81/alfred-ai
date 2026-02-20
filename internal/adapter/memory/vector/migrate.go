package vector

import "database/sql"

// migrate creates the schema if it doesn't exist.
func migrate(db *sql.DB) error {
	const schema = `
		CREATE TABLE IF NOT EXISTS entries (
			id         TEXT PRIMARY KEY,
			content    TEXT NOT NULL,
			tags       TEXT NOT NULL DEFAULT '[]',
			metadata   TEXT NOT NULL DEFAULT '{}',
			embedding  BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
			content, tags, content=entries, content_rowid=rowid
		);

		-- Triggers to keep FTS in sync with entries table.
		CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
			INSERT INTO entries_fts(rowid, content, tags) VALUES (new.rowid, new.content, new.tags);
		END;

		CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
			INSERT INTO entries_fts(entries_fts, rowid, content, tags) VALUES ('delete', old.rowid, old.content, old.tags);
		END;

		CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE ON entries BEGIN
			INSERT INTO entries_fts(entries_fts, rowid, content, tags) VALUES ('delete', old.rowid, old.content, old.tags);
			INSERT INTO entries_fts(rowid, content, tags) VALUES (new.rowid, new.content, new.tags);
		END;
	`
	_, err := db.Exec(schema)
	return err
}
