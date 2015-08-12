package ui

import (
	_ "fmt"
	"html/template"
	"net/http"
	_ "time"
)

type Page struct {
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

func StreamData(dataStream <-chan map[string]string) {
	go func() {
		for {
			newData, ok := <-dataStream
			if ok {
				p.Data = append(p.Data, newData)
			} else {
				break
			}
		}
	}()
}

// ImportData populates the data array
func ImportData(input [][]string) {
	p = &Page{Data: input}
}

// StartUIServer starts a server which will serve pages to localhost.
// ListenAndServe is run in its own goroutine
func StartUIServer() {
	p = &Page{Data: make([][]string, 0)}
	http.HandleFunc("/", tempHandler)
	go http.ListenAndServe(":8080", nil)
}
