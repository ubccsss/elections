package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
)

var (
	file                = flag.String("f", "", "the file to load to check against")
	studentNumberRegexp = regexp.MustCompile(`^\d{8}$`)
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%+v", err)
	}
}

func run() error {
	flag.Parse()

	color.NoColor = false

	body, err := ioutil.ReadFile(*file)
	if err != nil {
		return err
	}
	students := strings.Split(strings.TrimSpace(string(body)), "\n")
	for i, student := range students {
		students[i] = strings.TrimSpace(student)
	}

	studentsMap := map[string]struct{}{}
	for _, student := range students {
		studentsMap[student] = struct{}{}
	}

	for {
		validate := func(input string) error {
			if !studentNumberRegexp.MatchString(input) {
				return errors.New("invalid format; must be in 0000000")
			}
			return nil
		}

		prompt := promptui.Prompt{
			Label:    "Student Number 🍕🍕🍕",
			Validate: validate,
		}

		result, err := prompt.Run()
		if err != nil {
			return err
		}

		if _, ok := studentsMap[result]; ok {
			fmt.Printf("\n  👌👌👌 %s\n\n", color.GreenString("valid     "))
		} else {
			fmt.Printf("\n  🙅🙅🙅 %s\n\n", color.RedString("invalid       "))
		}
	}
	return nil
}
