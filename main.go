package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/table"
	"github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeConfigPath = flag.String("config-path", ".kube/config", "path to kubeconfig file")
	pattern        = flag.String("pattern", "qbox/qbox-docker:6.2.1", "pattern for recognizing elasticsearch containers")

	esCpu    int
	esMemory int

	totalCpu    int
	totalMemory int

	esCount    int
	totalCount int
)

func main() {
	flag.Parse()

	configBytes, err := ioutil.ReadFile(*kubeConfigPath)

	if err != nil {
		log.Fatalf("error reading file %s %v", kubeConfigPath, err)
	}

	kubeConfig, err := clientcmd.Load([]byte(configBytes))

	if err != nil {
		logrus.Fatalf("can't load kubernetes config %v", err)
	}

	restConf, err := clientcmd.NewNonInteractiveClientConfig(
		*kubeConfig,
		kubeConfig.CurrentContext,
		&clientcmd.ConfigOverrides{},
		nil,
	).ClientConfig()

	if err != nil {
		log.Fatalf("create rest config %v", err)
	}

	clientSet, err := kubernetes.NewForConfig(restConf)

	if err != nil {
		log.Fatalf("create client set %v", err)
	}

	nsList, err := clientSet.CoreV1().Namespaces().List(metav1.ListOptions{})

	if err != nil {
		log.Fatalf("list namespaces %v", err)
	}

	for _, ns := range nsList.Items {
		podList, err := clientSet.CoreV1().Pods(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}

		for _, pod := range podList.Items {
			for _, container := range pod.Spec.Containers {
				if strings.Contains(container.Image, *pattern) {
					esCpu += container.Resources.Limits.Cpu().Size()
					esMemory += container.Resources.Limits.Memory().Size()
					esCount += 1
				}

				totalCpu += container.Resources.Limits.Cpu().Size()
				totalMemory += container.Resources.Limits.Memory().Size()
				totalCount += 1
			}
		}
	}

	cpuRatio := float64(esCpu) / float64(totalCpu)
	memoryRatio := float64(esMemory) / float64(totalMemory)

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"#", "Total CPU", "Total Memory", "Target CPU", "Target Memory", "CPU %", "Memory %s"})
	t.AppendRows([]table.Row{
		{1, totalCpu, totalMemory, esCpu, esMemory,
			fmt.Sprintf("%.2f", cpuRatio), fmt.Sprintf("%.2f", memoryRatio)},
	})

	t.Render()
}
