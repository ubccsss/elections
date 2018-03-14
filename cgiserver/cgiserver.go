package main

import (
	"log"
	"net/http"
	"net/http/cgi"
	"os"
	"path/filepath"
	"strings"
)

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	path := r.URL.Path
	file := filepath.FromSlash(path)[1:]
	log.Printf("%s - %s", r.Method, file)

	cgiPath := ""

	parts := strings.Split(file, string(filepath.Separator))
	for i, part := range parts {
		if filepath.Ext(part) == ".cgi" {
			cgiPath = filepath.Join(parts[:i+1]...)
			break
		}
	}

	if len(cgiPath) > 0 {
		cgih := cgi.Handler{
			Path: cgiPath,
			Root: ".",
		}
		cgih.Env = append(cgih.Env, "REMOTE_USER="+r.FormValue("REMOTE_USER"))
		r.Form.Del("REMOTE_USER")
		cgih.ServeHTTP(w, r)
	} else {
		f, _ := os.Stat(file)
		if (f != nil && f.IsDir()) || file == "" {
			tmp := filepath.Join(file, "index.html")
			if _, err := os.Stat(tmp); err == nil {
				file = tmp
			}
		}
		http.ServeFile(w, r, file)
	}
}

func main() {
	if err := http.ListenAndServe(":8080", http.HandlerFunc(handleHTTP)); err != nil {
		log.Fatal(err)
	}
}
