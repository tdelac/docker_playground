package ui

import (
	_ "fmt"
	"html/template"
	"net/http"
)

type Page struct {
	Data [][]string
}

const (
	index = "public/static/templates/index.html"
)

var (
	p *Page
)

func tempHandler(w http.ResponseWriter, r *http.Request) {
	t, _ := template.ParseFiles(index)
	t.Execute(w, p)
}

// ImportData populates the data array
func ImportData(input [][]string) {
	p = &Page{Data: input}
}

// StartUIServer starts a server which will serve pages to localhost.
// ListenAndServe is run in its own goroutine
func StartUIServer() {
	http.HandleFunc("/", tempHandler)
	go http.ListenAndServe(":8080", nil)
}
