package main

import (
  "net/http" //package for http based web programs
  "fmt"
  "cs490/spdy"
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
  Handler http.Handler
  Addr string
}

func (alt SpdyAlternate) ServeHTTP(w http.ResponseWriter, req *http.Request) {
  w.Header().Set("Alternate-Protocol", alt.Addr)
  alt.Handler.ServeHTTP(w, req)
}

func main() {
  // add handlers...
  // go spdy.ListenAndServe(":8081", nil)
  spdy.ListenAndServe(":8081", nil)
  http.ListenAndServe(":8080",
  SpdyAlternate{http.DefaultServeMux, "8081:npn-spdy/2"})
}
