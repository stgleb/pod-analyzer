package agent

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/table"
	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
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

func getOutputWriter(outputFileName string) (io.WriteCloser, error) {
	if len(outputFileName) == 0 {
		return os.Stdout, nil
	}

	return os.OpenFile(outputFileName, os.O_CREATE|os.O_RDWR, 555)
}

func getClientSet(kubeConfigPath string) (*kubernetes.Clientset, error) {
	configBytes, err := ioutil.ReadFile(kubeConfigPath)

	if err != nil {
		return nil, errors.Wrapf(err, "error reading file %s %v", kubeConfigPath)
	}

	kubeConfig, err := clientcmd.Load([]byte(configBytes))

	if err != nil {
		return nil, errors.Wrapf(err, "can't load kubernetes config %v")
	}

	restConf, err := clientcmd.NewNonInteractiveClientConfig(
		*kubeConfig,
		kubeConfig.CurrentContext,
		&clientcmd.ConfigOverrides{},
		nil,
	).ClientConfig()

	if err != nil {
		return nil, errors.Wrapf(err, "create rest config %v")
	}

	clientSet, err := kubernetes.NewForConfig(restConf)

	if err != nil {
		log.Fatalf("create client set %v", err)
	}

	return clientSet, nil
}

func Run(kubeConfigPath, pattern, outputFileName string, hasReport bool) error {
	flag.Parse()

	clientSet, err := getClientSet(kubeConfigPath)

	if err != nil {
		return errors.Wrapf(err, "get client set")
	}

	nsList, err := clientSet.CoreV1().Namespaces().List(metav1.ListOptions{})

	if err != nil {
		return errors.Wrapf(err, "list namespaces")
	}

	containerCpuLimitInfo := make(map[string]int)
	containerMemoryLimitInfo := make(map[string]int)

	containerCpuRequestInfo := make(map[string]int)
	containerMemoryRequestInfo := make(map[string]int)

	detailedInfo := make(map[string]map[string]map[string]v1.ResourceRequirements)

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
				// Aggregate info about each container  image and usage
				containerCpuLimitInfo[container.Image] += container.Resources.Limits.Cpu().Size()
				containerMemoryLimitInfo[container.Image] += container.Resources.Limits.Memory().Size()

				containerCpuRequestInfo[container.Image] += container.Resources.Requests.Cpu().Size()
				containerMemoryRequestInfo[container.Image] += container.Resources.Requests.Memory().Size()

				// Count total amount of resources used limit/requests
				totalCpuLimits += container.Resources.Limits.Cpu().Size()
				totalMemoryLimits += container.Resources.Limits.Memory().Size()

				totalCpuRequests += container.Resources.Requests.Cpu().Size()
				totalMemoryRequests += container.Resources.Requests.Memory().Size()

				// Ensure that maps are  non-nil
				if detailedInfo[ns.Name] == nil {
					detailedInfo[ns.Name] = make(map[string]map[string]v1.ResourceRequirements)

					if detailedInfo[ns.Name][pod.Name] == nil {
						detailedInfo[ns.Name][pod.Name] = make(map[string]v1.ResourceRequirements)
					}
				}
				// Grab detailed info about resource usages
				detailedInfo[ns.Name][pod.Name][container.Image] = container.Resources
				totalCount += 1
			}
		}
	}

	cpuLimitRatio := float64(esCpuLimits) / float64(totalCpuLimits)
	memoryLimitRatio := float64(esMemoryLimits) / float64(totalMemoryLimits)

	cpuReqRatio := float64(esCpuRequests) / float64(totalCpuRequests)
	memoryReqRatio := float64(esMemoryRequests) / float64(totalCpuRequests)

	if f, err := getOutputWriter(outputFileName); err == nil {
		renderCSV(f, cpuLimitRatio, memoryLimitRatio, cpuReqRatio, memoryReqRatio,
			containerCpuLimitInfo, containerMemoryLimitInfo, containerMemoryRequestInfo, containerCpuRequestInfo)
	} else {
		return err
	}

	if f, err := getOutputWriter("report.json"); err == nil {
		if err := json.NewEncoder(f).Encode(detailedInfo); err != nil {
			return errors.Wrapf(err, "write a report")
		}
	} else {
		return errors.Wrapf(err, "get writer for report")
	}

	return nil
}

func renderCSV(out io.Writer, cpuLimitRatio float64, memoryLimitRatio float64, cpuReqRatio float64,
	memoryReqRatio float64, containerCpuLimitInfo map[string]int, containerMemoryLimitInfo map[string]int,
	containerMemoryRequestInfo map[string]int, containerCpuRequestInfo map[string]int) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"#", "Name", "Memory Limits", "CPU Limits", "Memory Requests", "CPU requests"})
	t.AppendRows([]table.Row{
		{1, "Total", totalCpuLimits, totalMemoryLimits, totalMemoryRequests, totalMemoryRequests},
		{2, "Target", esCpuLimits, esMemoryLimits, esMemoryRequests, esCpuRequests},
		{3, "Ratio", fmt.Sprintf("%.2f", cpuLimitRatio), fmt.Sprintf("%.2f", memoryLimitRatio),
			fmt.Sprintf("%.2f", cpuReqRatio), fmt.Sprintf("%.2f", memoryReqRatio)},
	})
	i := 4
	for imageName := range containerCpuLimitInfo {
		t.AppendRows([]table.Row{
			{i, imageName, containerMemoryLimitInfo[imageName], containerCpuLimitInfo[imageName],
				containerMemoryRequestInfo[imageName], containerCpuRequestInfo[imageName]},
		})
		i++
	}
	t.SetOutputMirror(out)
	t.RenderCSV()
}
