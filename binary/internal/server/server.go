package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/flosch/pongo2/v6"
)

func Start(port int) error {
	pagesDir := "pages"
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		return fmt.Errorf("pages directory not found")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		filePath := filepath.Join(pagesDir, filepath.Clean(path))

		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}

		tpl, err := pongo2.FromFile(filePath)
		if err != nil {
			http.Error(w, "Template error", 500)
			return
		}

		ctx := pongo2.Context{
			"site":    map[string]string{"name": "Friendo Site"},
			"request": map[string]string{"path": r.URL.Path},
		}

		if err := tpl.ExecuteWriter(ctx, w); err != nil {
			http.Error(w, "Render error", 500)
		}
	})

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Serving on http://localhost%s\n", addr)
	return http.ListenAndServe(addr, nil)
}
