package main

import (
	"fmt"
	"time"
)

func main() {
	var counter = 1
	for {
		fmt.Println("hello", counter)
		counter = counter + 1

		time.Sleep(time.Second)
	}
}
