package pkg

import (
	"fmt"
	"github.com/apoorvam/goterminal"
	"os"
)

var CONSOLE = goterminal.New(os.Stdout)

func logAction(format string, values ...interface{}) {
	CONSOLE.Clear()
	fmt.Fprintf(CONSOLE, format, values...)
	CONSOLE.Print()
}

func logSection(format string, values ...interface{}) {
	CONSOLE.Clear()
	fmt.Printf(format, values...)
}
