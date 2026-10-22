package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/p4gefau1t/trojan-go/component"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/option"
)

// flushOnSignal writes out the lines still queued in the asynchronous logger
// before the process is terminated by a signal
func flushOnSignal() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		log.Flush()
		os.Exit(0)
	}()
}

func main() {
	flag.Parse()
	flushOnSignal()
	for {
		h, err := option.PopOptionHandler()
		if err != nil {
			log.Fatal("invalid options")
		}
		err = h.Handle()
		if err == nil {
			break
		}
	}
	log.Flush()
}
