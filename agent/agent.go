package agent

import (
	"flag"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/table"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	esCpu    int
	esMemory int

	totalCpu    int
	totalMemory int

	esCount    int
	totalCount int
)

func Run(kubeConfigPath, pattern string) error {
	flag.Parse()

	containerCpuInfo := make(map[string]int)
	containerMemoryInfo := make(map[string]int)

	//  TODO(stgleb): Extract getting client set to separate function
	configBytes, err := ioutil.ReadFile(kubeConfigPath)

	if err != nil {
		return errors.Wrapf(err, "error reading file %s %v", kubeConfigPath)
	}

	kubeConfig, err := clientcmd.Load([]byte(configBytes))

	if err != nil {
		return errors.Wrapf(err, "can't load kubernetes config %v")
	}

	restConf, err := clientcmd.NewNonInteractiveClientConfig(
		*kubeConfig,
		kubeConfig.CurrentContext,
		&clientcmd.ConfigOverrides{},
		nil,
	).ClientConfig()

	if err != nil {
		return errors.Wrapf(err, "create rest config %v")
	}

	clientSet, err := kubernetes.NewForConfig(restConf)

	if err != nil {
		log.Fatalf("create client set %v", err)
	}

	// TODO(stgleb): extract analyze of single namespace to separate function
	nsList, err := clientSet.CoreV1().Namespaces().List(metav1.ListOptions{})

	if err != nil {
		return errors.Wrapf(err, "list namespaces")
	}

	for _, ns := range nsList.Items {
		podList, err := clientSet.CoreV1().Pods(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			return errors.Wrapf(err, "error getting list of pods")
		}

		for _, pod := range podList.Items {
			for _, container := range pod.Spec.Containers {
				containerCpuInfo[container.Image] += container.Resources.Limits.Cpu().Size()
				containerMemoryInfo[container.Image] += container.Resources.Limits.Memory().Size()

				if strings.Contains(container.Image, pattern) {
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

	// TODO(stgleb): extract rendering to separate function
	cpuRatio := float64(esCpu) / float64(totalCpu)
	memoryRatio := float64(esMemory) / float64(totalMemory)

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"#", "Name", "Memory", "CPU"})
	t.AppendRows([]table.Row{
		{1, "Total", totalCpu, totalMemory},
		{2, "Target", esCpu, esMemory},
		{3, "Ratio", fmt.Sprintf("%.2f", cpuRatio), fmt.Sprintf("%.2f", memoryRatio)},
	})

	i := 4

	for imageName := range containerCpuInfo {
		cpuValue := containerCpuInfo[imageName]
		memoryValue := containerMemoryInfo[imageName]
		t.AppendRows([]table.Row{{i, imageName, cpuValue, memoryValue}})
		i++
	}

	t.Render()
	return nil
}
