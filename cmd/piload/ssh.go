package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type sshCfg struct {
	Host, Port, User, Auth, KeyPath, Password string
}

func (c sshCfg) client() (*ssh.Client, error) {
	port := 22
	if n, err := strconv.Atoi(strings.TrimSpace(c.Port)); err == nil && n > 0 {
		port = n
	}
	var auth []ssh.AuthMethod
	if c.Auth == "key" && strings.TrimSpace(c.KeyPath) != "" {
		key, err := os.ReadFile(c.KeyPath)
		if err != nil {
			return nil, err
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, err
		}
		auth = append(auth, ssh.PublicKeys(signer))
	} else {
		auth = append(auth, ssh.Password(c.Password))
	}
	cfg := &ssh.ClientConfig{
		User:            strings.TrimSpace(c.User),
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         12 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", strings.TrimSpace(c.Host), port)
	return ssh.Dial("tcp", addr, cfg)
}

func sshRun(cfg sshCfg, command string, timeout time.Duration) (string, string, int, error) {
	client, err := cfg.client()
	if err != nil {
		return "", "", -1, err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return "", "", -1, err
	}
	defer sess.Close()
	if timeout > 0 {
		timer := time.AfterFunc(timeout, func() { _ = sess.Close() })
		defer timer.Stop()
	}
	var stdout, stderr strings.Builder
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	err = sess.Run(command)
	code := 0
	if err != nil {
		if ee, ok := err.(*ssh.ExitError); ok {
			code = ee.ExitStatus()
		} else {
			return stdout.String(), stderr.String(), -1, err
		}
	}
	return stdout.String(), stderr.String(), code, nil
}

func sshStream(cfg sshCfg, command string, onLine func(string)) (int, error) {
	client, err := cfg.client()
	if err != nil {
		return -1, err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return -1, err
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := sess.Start(command); err != nil {
		return -1, err
	}
	done := make(chan struct{}, 2)
	read := func(r interface{ Read([]byte) (int, error) }) {
		buf := make([]byte, 4096)
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
	go read(stdout)
	go read(stderr)
	err = sess.Wait()
	<-done
	<-done
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*ssh.ExitError); ok {
		return ee.ExitStatus(), nil
	}
	return -1, err
}
