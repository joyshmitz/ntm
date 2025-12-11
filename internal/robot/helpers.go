package robot

import (
	"os"
)

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
