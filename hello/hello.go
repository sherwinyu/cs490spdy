package main

import (
  "fmt"
  "time"
)


type Tree struct {
  Left  *Tree
  Value int
  Right *Tree
}

func main() {
  tick := time.Tick(1e8)
  boom := time.After(5e9)
  for {
    select {
    case <-tick:
      fmt.Println("tick.")
    case <-boom:
      fmt.Println("BOOM!")
      return
    default:
      fmt.Println("    .")
      time.Sleep(5e7)
    }
  }
}

