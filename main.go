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

func run(ctx context.Context, run, grace time.Duration, args []string) error {
	runT := time.NewTicker(run)
	defer runT.Stop()

	// start program in a separate process group to prevent automatic signals propagation
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// receive program exit status asynchronously
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// wait for ctx to be canceled, program to exit, or for runT to tick
	select {
	case <-ctx.Done():
		// nothing
	case err := <-done:
		return err
	case <-runT.C:
		// nothing
	}

	// ask program to exit
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to send SIGTERM: %s", err)
	}

	// wait for program to exit, or for graceT to tick; ignore ctx even if it is already canceled
	graceT := time.NewTicker(grace)
	defer graceT.Stop()
	select {
	case err := <-done:
		return err
	case <-graceT.C:
		// nothing
	}

	// kill program
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		log.Printf("Failed to send SIGKILL: %s", err)
	}

	// wait for program to exit
	return <-done
}

func main() {
	runF := flag.Duration("run", time.Minute, "Period between starting a program and sending it SIGTERM")
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

	ctx, cancel := context.WithCancel(context.Background())

	// handle termination signals: first one gracefully, force exit on the second one
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-signals
		log.Printf("Got %v (%d) signal, shutting down...", s, s.(syscall.Signal))
		cancel()

		s = <-signals
		log.Panicf("Got %v (%d) signal, exiting!", s, s.(syscall.Signal))
	}()

	for {
		if err := run(ctx, *runF, *graceF, flag.Args()); err != nil {
			log.Fatal(err)
		}
	}
}
