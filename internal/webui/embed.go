package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html favicon.ico manifest.json sw.js css/* js/* img/* vendor/*
var assets embed.FS

func FileHandler() http.Handler {
	sub, _ := fs.Sub(assets, ".")
	return http.FileServer(http.FS(sub))
}
