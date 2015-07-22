package ui

import (
	"html/template"
	"net/http"
)

const (
	index = "../public/static/templates/index.html"
)

func tempHandler(w http.ResponseWriter, r *http.Request) {
	t, _ := template.ParseFiles("index.html")
	t.Execute(w, p)
}

func StartUIServer() {
	http.HandleFunc("/", tempHandler)
	http.ListenAndServe(":8080", nil)
}
