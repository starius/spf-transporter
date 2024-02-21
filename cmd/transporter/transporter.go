package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	goflags "github.com/jessevdk/go-flags"
	"gitlab.com/scpcorp/spf-transporter/app"
)

func parse(config interface{}) {
	errWrongCommand := 2

	_, err := goflags.Parse(config)
	if err != nil {
		if err, ok := err.(*goflags.Error); ok && err.Type == goflags.ErrHelp {
			os.Exit(errWrongCommand)
		}
		log.Fatalf("Error during flags parsing: %v.", err)
	}
}

func waitForShutdown(f func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	s, ok := <-c
	if !ok {
		return
	}
	log.Printf("Got signal: %v", s)

	f()
}

func main() {
	log.SetFlags(0)
	var config app.Config
	parse(&config)

	a := app.New()
	if err := a.Start(config); err != nil {
		log.Printf("Failed to start transporter application: %v", err)
		os.Exit(1)
	}

	waitForShutdown(a.Close)
}
