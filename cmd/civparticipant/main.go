// civparticipant contains no guardian or consensus signing material.
package main

import (
	"civora/internal/codec"
	"civora/internal/participation"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "bundle" {
		return participation.Bundle(os.Args[2:])
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("civora-reseau", flag.ContinueOnError)
	config := flags.String("config", filepath.Join(filepath.Dir(exe), "reseau.json"), "configuration included in the archive")
	home := flags.String("home", filepath.Join(filepath.Dir(exe), "donnees"), "persistent local archive")
	once := flags.Bool("once", false, "verify and announce once, then exit")
	interval := flags.Duration("interval", 10*time.Second, "synchronization interval (2s to 30s)")
	if err = flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *interval < 2*time.Second || *interval > 30*time.Second {
		return fmt.Errorf("invalid arguments or interval")
	}
	raw, err := os.ReadFile(*config)
	if err != nil {
		return err
	}
	observer, err := participation.Open(raw, *home)
	if err != nil {
		return err
	}
	defer observer.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Println("Civora — observateur du réseau expérimental local (PUBLIC_TEST_ONLY)")
	fmt.Println("Vérification et conservation du registre. Ctrl+C pour arrêter et reprendre plus tard.")
	fmt.Println("Données locales :", *home)
	for {
		view, syncErr := observer.Sync()
		if syncErr == nil {
			syncErr = observer.Announce(ctx, view, "online")
		}
		if syncErr != nil {
			if *once {
				return syncErr
			}
			fmt.Fprintln(os.Stderr, "En attente du réseau :", syncErr)
		} else {
			fmt.Println(string(codec.Must(map[string]any{"network_id": observer.Config.NetworkID, "node_id": observer.NodeID(), "height": view.Height, "app_hash": view.Root, "connected": true, "role": "observer"})))
			if *once {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			if view != nil {
				leave, stop := context.WithTimeout(context.Background(), 3*time.Second)
				_ = observer.Announce(leave, view, "offline")
				stop()
			}
			return nil
		case <-time.After(*interval):
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
