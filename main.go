package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func run(ctx context.Context, run time.Duration, args []string) error {
	t := time.NewTicker(run)
	defer t.Stop()

	// start program
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	// wait for program to exit or for t to tick
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-t.C:
		// nothing
	}

	// signal program to exit
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to send SIGTERM: %s", err)
	}

	// wait for program to exit from SIGTERM or from SIGKILL from the ctx
	return <-done
}

func main() {
	runF := flag.Duration("run", time.Minute, "Periad between starting a program and sending it SIGTERM")
	graceF := flag.Duration("grace", 10*time.Second, "Period between sending a program SIGTERM and SIGKILL")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] [program] [program arguments]\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	log.SetPrefix("ruc: ")
	log.SetFlags(log.Ltime)

	mainCtx, mainCancel := context.WithCancel(context.Background())

	// handle termination signals: first one gracefully, force exit on the second one
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-signals
		log.Printf("Got %v (%d) signal, shutting down...", s, s.(syscall.Signal))
		mainCancel()

		s = <-signals
		log.Panicf("Got %v (%d) signal, exiting!", s, s.(syscall.Signal))
	}()

	for {
		log.Printf("Starting...")
		ctx, cancel := context.WithTimeout(mainCtx, *runF+*graceF)
		if err := run(ctx, *runF, flag.Args()); err != nil {
			log.Fatal(err)
		}
		cancel()
	}
}
