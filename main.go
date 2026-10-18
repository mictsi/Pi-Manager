package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

var version = "0.2.8"

//go:embed assets/THIRD_PARTY.txt
var thirdPartyNotices string

//go:embed LICENSE
var projectLicense string

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pi-manager:", err)
		showStartupError(err.Error())
		os.Exit(1)
	}
}

func run() error {
	debug := flag.Bool("debug", false, "Log read requests as well as actions and errors")
	showVersion := flag.Bool("version", false, "Print version and exit")
	showLicenses := flag.Bool("licenses", false, "Print project and third-party licenses")
	flag.Parse()
	if *showLicenses {
		fmt.Print(projectLicense + "\n" + thirdPartyNotices)
		return nil
	}
	if *showVersion {
		fmt.Println("Pi-Manager", version)
		return nil
	}
	started, err := launchDetached()
	if err != nil {
		return err
	}
	if started {
		return nil
	}
	instance, err := acquireSingleInstance()
	if err != nil {
		return fmt.Errorf("cannot acquire application instance lock: %w", err)
	}
	if instance == nil {
		// Another desktop instance, including one minimized to tray, owns the lock.
		return nil
	}
	defer instance.Close()
	dir, err := executableDir()
	if err != nil {
		return err
	}
	log, writer, err := openLog(filepath.Join(dir, "pi-manager.log"), *debug)
	if err != nil {
		return fmt.Errorf("cannot open log beside the binary: %w", err)
	}
	defer writer.Close()
	cfg, err := openConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		log.Error("configuration could not be loaded", "error", err.Error())
		return err
	}
	a := newApp(cfg, log)
	defer a.Close()
	log.Info("application started", "version", version, "pid", os.Getpid())
	defer log.Info("application stopped")
	err = runDesktop(a)
	if err != nil {
		log.Error("desktop stopped with an error", "error", err.Error())
	}
	return err
}
