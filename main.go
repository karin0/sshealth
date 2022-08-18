package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"golang.org/x/crypto/ssh"

	knownhosts "golang.org/x/crypto/ssh/knownhosts"
)

func main() {
	var keyFile string
	var knownHostsFile string
	var addr string
	var user string
	flag.StringVar(&keyFile, "key", "", "private key file")
	flag.StringVar(&knownHostsFile, "hosts", "", "known_hosts file containing the host keys")
	flag.StringVar(&addr, "addr", "", "hostname:port")
	flag.StringVar(&user, "user", "sshealth", "user")
	flag.Parse()

	if keyFile == "" || addr == "" {
		flag.Usage()
		return
	}

	if knownHostsFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("homedir: %v", err)
		}
		knownHostsFile = home + "/.ssh/known_hosts"
	}

	hostKeyCallback, err := knownhosts.New(knownHostsFile)
	if err != nil {
		log.Fatalf("load known_hosts: %v", err)
	}

	key, err := ioutil.ReadFile(keyFile)
	if err != nil {
		log.Fatalf("read private key: %v", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		log.Fatalf("parse private key: %v", err)
	}

	config := ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Second * 20,
	}

	runner := func() error {
		client, err := ssh.Dial("tcp", addr, &config)
		if err != nil {
			return fmt.Errorf("ssh dial: %w", err)
		}

		session, err := client.NewSession()
		defer session.Close()
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		out, err := session.StdoutPipe()
		if err != nil {
			return fmt.Errorf("pipe: %w", err)
		}

		err = session.Start("")
		if err != nil {
			return fmt.Errorf("start: %w", err)
		}
		log.Printf("session started to %s @ %s", user, addr)

		ch := make(chan int)
		go func() {
			var resp [10]byte
			for {
				_, err := out.Read(resp[:])
				if err != nil {
					log.Printf("read: %s", err.Error())
					ch <- 1
					return
				}
				ch <- 0
			}
		}()

		ok := false
		timer := time.NewTimer(time.Second * 60)
	F:
		for {
			select {
			case <-timer.C:
				log.Printf("stop, timed up!")
				break F
			case x := <-ch:
				if x != 0 {
					log.Printf("stop, read error!")
					break F
				}
				ok = true
				timer.Reset(time.Second * 60)
			}
		}
		if !ok {
			return fmt.Errorf("no response")
		}
		return nil
	}

	for {
		err := runner()
		if err != nil {
			log.Printf("first error: %s", err.Error())
			err = runner()
			if err != nil {
				log.Fatalf("peer down!! %s", err.Error())
			}
		}
		time.Sleep(time.Minute)
	}
}
