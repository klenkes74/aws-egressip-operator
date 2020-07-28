package main

import (
	"fmt"
	"strings"
)

func main() {
	a3 := []string{ "a", "b", "c"}


	e := fmt.Errorf("Dies ist ein Fehler: %v", a3)

	fmt.Println(e.Error())

	if strings.Contains(e.Error(), "ist ein Fehler") {
		fmt.Println("ok")
	} else {
		fmt.Println("nack")
	}
}

