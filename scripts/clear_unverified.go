package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/glebarez/go-sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "ledger.db?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		log.Fatalf("FATAL: Failed to open SQLite ledger.db: %v", err)
	}
	defer db.Close()

	res, err := db.Exec("DELETE FROM internal_state WHERE status != 'VERIFIED'")
	if err != nil {
		log.Fatalf("FATAL: Failed to delete unverified entries: %v", err)
	}

	rows, _ := res.RowsAffected()
	fmt.Printf("Cleared %d unverified entries\n", rows)
}
