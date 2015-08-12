package main

import (
	"bufio"
	"fmt"
	"github.com/tdelac/docker_playground/tokenizer"
	"io"
	"os"
)

func parse(input string) error {
	tokens := tokenizer.New(input)
	for tokens.HasNext() {
		token, err := tokens.Next()
		if err != nil {
			return fmt.Errorf("Parse error: %v", err)
		}
		fmt.Printf("Token: '%s' retrieved\n", token)
	}

	return nil
}

func quit() {
	fmt.Printf("\nGoodbye!\n")
}

func main() {
	fmt.Printf("Welcome to the Remote Docker Shell\n")
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("$ ")
		input, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				quit()
				return
			}
			fmt.Printf("Improper read '%s': %v\n", input, err)
			os.Exit(1)
		}
		err = parse(input)
		if err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	}
}
