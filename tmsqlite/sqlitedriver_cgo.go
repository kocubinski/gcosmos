//go:build cgo && !purego

package tmsqlite

import (
	_ "github.com/mattn/go-sqlite3"
)

const (
	sqliteDriverType = "sqlite3"
	sqliteBuildType  = "cgo"
)
