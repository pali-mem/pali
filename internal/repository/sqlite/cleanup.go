package sqlite

import "database/sql"

func closeDB(db *sql.DB) {
	if db != nil {
		_ = db.Close()
	}
}

// CloseDBForTest lets other packages reuse the standard test cleanup behavior.
func CloseDBForTest(db *sql.DB) {
	closeDB(db)
}

func closeRows(rows *sql.Rows) {
	if rows != nil {
		_ = rows.Close()
	}
}

func rollbackTx(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}
