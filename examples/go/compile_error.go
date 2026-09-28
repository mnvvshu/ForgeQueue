//go:build ignore

// examples/go/compile_error.go
package main

func main() {
	// Undefined variable causes compile failure
	fmt.Println(undefinedVariable)
}
