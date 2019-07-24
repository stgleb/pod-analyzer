package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/stgleb/pod-analyzer/agent"

	scp "github.com/bramvdbogaerde/go-scp"
	"golang.org/x/crypto/ssh"
)

var (
	regionListFileName = flag.String("regionListFileName", "regions.txt", "list of regions")
	username           = flag.String("username", "user", "user name for ssh login")
	password           = flag.String("password", "1234", "password for user")
	outputFile         = flag.String("output", "out.csv", "name of csv output file")
	mode               = flag.String("mode", "server", "mode server(default) and agent for grabbing info")
	pathToRemoteFile   = flag.String("remoteBin", "/tmp/agent", "path to remote binary")

	// Agent params
	kubeConfigPath = flag.String("config-path", ".kube/config", "path to kubeconfig file")
	pattern        = flag.String("pattern", "qbox/qbox-docker:6.2.1", "pattern for recognizing elasticsearch containers")
)

func copyAgentBinary(host, pathToLocalFile, pathToRemoteFile string, clientConfig *ssh.ClientConfig) error {
	client := scp.NewClient(fmt.Sprintf("%s:22", host), clientConfig)

	// Connect to the remote server
	err := client.Connect()
	if err != nil {
		return errors.Wrapf(err, "Couldn't establish a connection to the remote server ")
	}

	// Open a file
	f, err := os.Open(pathToLocalFile)

	// Close client connection after the file has been copied
	defer client.Close()

	// Close the file after it has been copied
	defer f.Close()

	err = client.CopyFile(f, pathToRemoteFile, "0755")

	if err != nil {
		return errors.Wrapf(err, "error while copying file ")
	}

	return nil
}

func readRegions(fileName string) ([]string, error) {
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
	// Setup configuration for SSH client
	config := &ssh.ClientConfig{
		Timeout: time.Second * 10,
		User:    *username,
		Auth: []ssh.AuthMethod{
			ssh.Password(*password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	regionList, err := readRegions(*regionListFileName)

	if err != nil {
		log.Fatalf("err reading region regionListFileName %v", err)
	}

	outputFile, err := os.OpenFile(*outputFile, os.O_RDWR|os.O_CREATE, 600)

	if err != nil {
		log.Fatalf("open output file %v\n", err)
	}

	for _, region := range regionList {
		pathToLocalFile, err := os.Executable()

		if err != nil {
			log.Fatalf("find path to local file %v", err)
		}

		if err := copyAgentBinary(region, pathToLocalFile, *pathToRemoteFile, config); err != nil {
			log.Fatalf("error copying binary %s to remote server %s:%s %v")
		}

		conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", region), config)

		if err != nil {
			log.Printf("error connect to region %s %v\n", region, err)
			continue
		}

		session, err := conn.NewSession()

		if err != nil {
			log.Printf("error open session to region %s %v\n", region, err)
			continue
		}

		stdin, err := session.StdoutPipe()

		if err != nil {
			log.Printf("error getting stdout pipe for %s %v\n", region, err)
			continue
		}

		command := fmt.Sprintf("%s -config-path=%s -pattern=%s", *pathToRemoteFile, *kubeConfigPath, *pattern)

		if err := session.Run(command); err != nil {
			log.Printf("error executing command %s %v", command, err)
			continue
		}

		go io.Copy(outputFile, stdin)
	}
}

func main() {
	flag.Parse()

	if *mode == "server" {
		analyzeClusters()
	} else {
		if err := agent.Run(*kubeConfigPath, *pattern); err != nil {
			log.Fatalf("error running agent %v", err)
		}
	}
}
