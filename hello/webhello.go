package main

import (
  "net/http" //package for http based web programs
  "fmt"
  "cs490/spdy"
  "html"
  "log"
)

func handler(w http.ResponseWriter, r *http.Request) {
  fmt.Println("Inside handler")
  fmt.Fprintf(w, "Hello world from my Go program!")
  fmt.Fprintf(w, "derpHello world from my Go program!")
}

/*
func main() {
  http.HandleFunc("/", handler) // redirect all urls to the handler function
  http.ListenAndServe("localhost:9999", nil) // listen for connections at port 9999 on the local machine
}
*/


type SpdyAlternate struct {
  http.Handler
  Addr string
}

func (alt SpdyAlternate) ServeHTTP(w http.ResponseWriter, req *http.Request) {
  w.Header().Set("Alternate-Protocol", alt.Addr)
  alt.Handler.ServeHTTP(w, req)
}

func main() {
  var _ = new(spdy.Server)
  fmt.Println("hello")
  // add handlers...
  go spdy.ListenAndServe("0.0.0.0:5555", nil)

  http.HandleFunc("/bar", func(w http.ResponseWriter, r *http.Request) {
      fmt.Println("hello...")
      fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
    })
    log.Fatal(http.ListenAndServe("0.0.0.0:8000", SpdyAlternate{http.DefaultServeMux, "5555:npn-spdy/2"}))
  // http.HandleFunc("/", handler) // redirect all urls to the handler function
  // http.ListenAndServe("0.0.0.0:8000", nil) // listen for connections at port 9999 on the local machine
  // spdy.ListenAndServe("0.0.0.0:8081", nil)
  // http.ListenAndServe("0.0.0.0:8080", SpdyAlternate{http.DefaultServeMux, "8081:npn-spdy/2"} )
  fmt.Println("hello")
}
