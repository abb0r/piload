package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

func runLocalYTDLP(args []string, onLine func(string)) (int, error) {
	cmd := exec.Command(ytdlpPath(), args...)
	hideWindow(cmd)
	if dir, err := toolsDir(); err == nil {
		cmd.Env = envWithBin(dir)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	done := make(chan struct{}, 2)
	go readLines(stdout, onLine, done)
	go readLines(stderr, onLine, done)
	err = cmd.Wait()
	<-done
	<-done
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

func readLines(r io.Reader, onLine func(string), done chan struct{}) {
	buf := make([]byte, 8192)
	var carry string
	for {
		n, err := r.Read(buf)
		if n > 0 {
			carry += string(buf[:n])
			for {
				i := strings.IndexByte(carry, '\n')
				if i < 0 {
					break
				}
				line := strings.TrimRight(carry[:i], "\r")
				if strings.TrimSpace(line) != "" {
					onLine(line)
				}
				carry = carry[i+1:]
			}
		}
		if err != nil {
			if strings.TrimSpace(carry) != "" {
				onLine(strings.TrimRight(carry, "\r"))
			}
			done <- struct{}{}
			return
		}
	}
}

func envWithBin(bin string) []string {
	sep := string(os.PathListSeparator)
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		key, val, ok := strings.Cut(e, "=")
		if ok && strings.EqualFold(key, "PATH") {
			out = append(out, key+"="+bin+sep+val)
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		out = append(out, "PATH="+bin)
	}
	return out
}
