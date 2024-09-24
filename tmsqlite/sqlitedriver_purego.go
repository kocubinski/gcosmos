//go:build purego || !cgo

package tmsqlite

import (
	_ "modernc.org/sqlite"
)

const (
	sqliteDriverType = "sqlite"
	sqliteBuildType  = "purego"
)
