package main

import (
	"clustta/internal/server/server_service"
	"os"

	"github.com/jmoiron/sqlx"
)

func main() {

	//get first argument
	if len(os.Args) < 2 {
		println("must provide studio or personal argument")
		return
	}

	serverDB := os.Args[1]
	studioName := os.Args[2]
	if studioName == "" {
		println("must provide studio name key")
		return
	}

	db, err := sqlx.Open("sqlite3", serverDB)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		panic(err)
	}
	defer tx.Rollback()

	println(studioName)
	println(serverDB)

	studioKey, err := server_service.GenerateStudioKey(tx, studioName)
	if err != nil {
		panic(err)
	}
	err = tx.Commit()
	if err != nil {
		panic(err)
	}
	println(studioKey)

}
