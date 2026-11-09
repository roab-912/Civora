// Civora's Windows launcher owns a Job Object so closing its window also stops
// its descendants. Existing servers reused by the JS runner stay independent.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func launch(lines <-chan string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	root := filepath.Dir(exe)
	script := filepath.Join(root, "scripts", "launch-wifi.mjs")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("conserver Civora-WiFi.exe a la racine du dossier Civora (scripts/launch-wifi.mjs introuvable)")
	}
	node, err := exec.LookPath("node.exe")
	if err != nil {
		return fmt.Errorf("Node.js >= 24.12 doit etre installe sur ce PC")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	cmd := exec.Command(node, append([]string{script}, os.Args[1:]...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CIVORA_WIFI_MANAGED=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	if err = cmd.Start(); err != nil {
		return err
	}
	// The runner waits for START before doing any work or spawning descendants.
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, handle)
		windows.CloseHandle(handle)
	}
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("protection des processus enfants : %w", err)
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	if _, err = io.WriteString(input, "START\n"); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case err = <-done:
			return err
		case _, open := <-lines:
			if !open {
				lines = nil
			} else {
				io.WriteString(input, "STOP\n")
			}
		case <-interrupt:
			io.WriteString(input, "STOP\n")
			select {
			case err = <-done:
				return err
			case <-time.After(12 * time.Second):
				return fmt.Errorf("arret force des processus du lanceur")
			}
		}
	}
}

func main() {
	lines := make(chan string, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	if err := launch(lines); err != nil {
		fmt.Fprintln(os.Stderr, "Erreur :", err)
		pause := true
		for _, arg := range os.Args[1:] {
			if arg == "--check" || arg == "--no-browser" || arg == "--help" {
				pause = false
			}
		}
		if pause {
			fmt.Println("Appuyer sur Entree pour fermer.")
			<-lines
		}
		os.Exit(1)
	}
}
