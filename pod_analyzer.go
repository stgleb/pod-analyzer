package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/stgleb/pod-analyzer/agent"

	scp "github.com/bramvdbogaerde/go-scp"
	"golang.org/x/crypto/ssh"
)

var (
	hostsFileName    = flag.String("hosts", "hosts.txt", "list of hosts")
	username         = flag.String("username", "root", "user name for ssh login")
	privateKeyFile   = flag.String("privateKey", "/home/stgleb/.ssh/id_rsa", "path to rsa private key")
	mode             = flag.String("mode", "server", "mode server(default) and agent for grabbing info")
	pathToRemoteFile = flag.String("remoteBin", "/tmp/agent", "path to remote binary")
	outputFileName = flag.String("output", "", "path to result file, default stdout")
	report = flag.Bool("report", false, "make a report to report.json file")

	// Agent params
	kubeConfigPath = flag.String("config-path", ".kube/config", "path to kubeconfig file")
	pattern        = flag.String("pattern", "qbox/qbox-docker:6.2.1", "pattern for recognizing elasticsearch containers")
)

func copyAgentBinary(host, pathToLocalFile, pathToRemoteFile string, clientConfig *ssh.ClientConfig) (err error) {
	client := scp.NewClient(fmt.Sprintf("%s:22", host), clientConfig)
	// Connect to the remote server
	err = client.Connect()
	if err != nil {
		return errors.Wrapf(err, "Couldn't establish a connection to the remote server ")
	}

	// Open a file
	f, err := os.Open(pathToLocalFile)

	// Close client connection after the file has been copied
	defer client.Close()

	// Close the file after it has been copied
	defer func() {
		if e := f.Close(); e != nil {
			log.Printf("error while closing %v", e)
			err = e
		}
	}()

	err = client.CopyFile(f, pathToRemoteFile, "0755")

	if err != nil {
		return errors.Wrapf(err, "error while copying file ")
	}

	return nil
}

func readHosts(fileName string) ([]string, error) {
	var list []string

	file, err := os.Open(fileName)

	if err != nil {
		log.Fatal(err)
	}

	defer func() {
		if err = file.Close(); err != nil {
			log.Printf("error closing file %v", err)
		}
	}()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		list = append(list, scanner.Text())
	}

	return list, scanner.Err()
}

func analyzeClusters() {
	privateKeyBytes, err := ioutil.ReadFile(*privateKeyFile)

	if err != nil {
		log.Fatalf("read private key %s error %v", *privateKeyFile, err)
	}

	key, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		log.Fatalf("error parse private key")
	}

	// Setup configuration for SSH client
	config := &ssh.ClientConfig{
		User: *username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
		Timeout: 30 * time.Second,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			logrus.Debugf("hostname %s,addr %s key %s", hostname, remote.String(), string(key.Type()))
			return nil
		},
		BannerCallback: func(message string) error {
			logrus.Debug(message)
			return nil
		},
	}

	hostList, err := readHosts(*hostsFileName)

	if err != nil {
		log.Fatalf("err reading host hostsFileName %v", err)
	}

	for _, host := range hostList {
		pathToLocalFile, err := os.Executable()

		if err != nil {
			log.Fatalf("find path to local file %v", err)
		}

		log.Printf("Copying binary %s to remote host %s", pathToLocalFile, host)
		if err := copyAgentBinary(host, pathToLocalFile, *pathToRemoteFile, config); err != nil {
			log.Fatalf("error copying binary %s to remote server %s:%s %v", pathToLocalFile, host,
				*pathToRemoteFile, err)
		}

		conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", host), config)

		if err != nil {
			log.Printf("error connect to host %s %v\n", host, err)
			continue
		}

		session, err := conn.NewSession()

		if err != nil {
			log.Printf("error open session to host %s %v\n", host, err)
			continue
		}

		stdout, err := session.StdoutPipe()

		if err != nil {
			log.Printf("error getting stdout pipe for %s %v\n", host, err)
			continue
		}

		command := fmt.Sprintf("%s -config-path=%s -pattern=%s -mode=agent", *pathToRemoteFile, *kubeConfigPath, *pattern)
		log.Printf("Host %s:22 Execute commanf %s", host, command)
		if err := session.Run(command); err != nil {
			log.Printf("error executing command %s %v", command, err)
			continue
		}

		ch := make(chan error)
		go func() {
			_, err := io.Copy(os.Stdout, stdout)
			ch <- err
			close(ch)
		}()

		select {
		case <-ch:
			log.Printf("finished")
		case <-time.After(time.Minute * 5):
			log.Printf("timeout")
		}
	}
}

func main() {
	flag.Parse()

	if *mode == "server" {
		analyzeClusters()
	} else {
		if err := agent.Run(*kubeConfigPath, *pattern, *outputFileName, *report); err != nil {
			log.Fatalf("error running agent %v", err)
		}
	}
}
