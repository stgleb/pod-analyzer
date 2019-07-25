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
	esCpuLimits    int
	esMemoryLimits int

	totalCpuLimits    int
	totalMemoryLimits int

	esCpuRequests    int
	esMemoryRequests int

	totalCpuRequests    int
	totalMemoryRequests int

	esCount    int
	totalCount int
)

func Run(kubeConfigPath, pattern string) error {
	flag.Parse()

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
				container.Resources.Requests.Cpu().Size()

				if strings.Contains(container.Image, pattern) {
					esCpuLimits += container.Resources.Limits.Cpu().Size()
					esMemoryLimits += container.Resources.Limits.Memory().Size()

					esCpuRequests += container.Resources.Requests.Cpu().Size()
					esMemoryRequests += container.Resources.Requests.Memory().Size()

					esCount += 1
				}

				totalCpuLimits += container.Resources.Limits.Cpu().Size()
				totalMemoryLimits += container.Resources.Limits.Memory().Size()

				totalCpuRequests += container.Resources.Requests.Cpu().Size()
				totalMemoryRequests += container.Resources.Requests.Memory().Size()

				totalCount += 1
			}
		}
	}

	cpuLimitRatio := float64(esCpuLimits) / float64(totalCpuLimits)
	memoryLimitRatio := float64(esMemoryLimits) / float64(totalMemoryLimits)

	cpuReqRatio := float64(esCpuRequests) / float64(totalCpuRequests)
	memoryReqRatio := float64(esMemoryRequests) / float64(totalCpuRequests)

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"#", "Name", "Memory Limits", "CPU Limits", "Memory Requests", "CPU requests"})
	t.AppendRows([]table.Row{
		{1, "Total", totalCpuLimits, totalMemoryLimits, totalMemoryRequests, totalMemoryRequests},
		{2, "Target", esCpuLimits, esMemoryLimits, esMemoryRequests, esCpuRequests},
		{3, "Ratio", fmt.Sprintf("%.2f", cpuLimitRatio), fmt.Sprintf("%.2f", memoryLimitRatio),
			fmt.Sprintf("%.2f", cpuReqRatio), fmt.Sprintf("%.2f", memoryReqRatio)},
	})

	t.Render()
	return nil
}
