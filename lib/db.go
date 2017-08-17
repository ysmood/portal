package lib

import (
	"github.com/syndtr/goleveldb/leveldb"
)

var db *leveldb.DB

// CloseDb ...
func CloseDb() {
	db.Close()
}

func initDb(dbPath string) {
	var err error

	db, err = leveldb.OpenFile(dbPath, nil)

	if err != nil {
		panic(err)
	}
}
