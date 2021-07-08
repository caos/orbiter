package main

import (
	"github.com/caos/orbos/cmd/chore"
)

func main() {
	if err := chore.BuildExecutables(true, true); err != nil {
		panic(err)
	}
}
