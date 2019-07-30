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

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	esCpuLimits    resource.Quantity
	esMemoryLimits resource.Quantity

	totalCpuLimits    resource.Quantity
	totalMemoryLimits resource.Quantity

	esCpuRequests    resource.Quantity
	esMemoryRequests resource.Quantity

	totalCpuRequests    resource.Quantity
	totalMemoryRequests resource.Quantity

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

	containerCpuLimitInfo := make(map[string]*resource.Quantity)
	containerMemoryLimitInfo := make(map[string]*resource.Quantity)

	containerCpuRequestInfo := make(map[string]*resource.Quantity)
	containerMemoryRequestInfo := make(map[string]*resource.Quantity)

	detailedInfo := make(map[string]map[string]map[string]v1.ResourceRequirements)

	for _, ns := range nsList.Items {
		podList, err := clientSet.CoreV1().Pods(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			return errors.Wrapf(err, "error getting list of pods")
		}

		// Ensure that maps are  non-nil
		if detailedInfo[ns.Name] == nil {
			detailedInfo[ns.Name] = make(map[string]map[string]v1.ResourceRequirements)
		}

		for _, pod := range podList.Items {
			// Ensure that maps are  non-nil
			if detailedInfo[ns.Name][pod.Name] == nil {
				detailedInfo[ns.Name][pod.Name] = make(map[string]v1.ResourceRequirements)
			}

			for _, container := range pod.Spec.Containers {
				container.Resources.Requests.Cpu().Size()

				if strings.Contains(container.Image, pattern) {
					esCpuLimits.Add(*container.Resources.Limits.Cpu())
					esMemoryLimits.Add(*container.Resources.Limits.Memory())

					esCpuRequests.Add(*container.Resources.Requests.Cpu())
					esMemoryRequests.Add(*container.Resources.Requests.Memory())

					esCount += 1
				}
				// Aggregate info about each container  image and usage
				if containerCpuLimitInfo[container.Image] == nil {
					containerCpuLimitInfo[container.Image] = container.Resources.Limits.Cpu()
				} else {
					containerCpuLimitInfo[container.Image].Add(*container.Resources.Limits.Cpu())
				}

				if containerMemoryLimitInfo[container.Image] == nil {
					containerMemoryLimitInfo[container.Image] = container.Resources.Limits.Memory()
				} else {
					containerMemoryLimitInfo[container.Image].Add(*container.Resources.Limits.Memory())
				}

				if containerCpuRequestInfo[container.Image] == nil {
					containerCpuRequestInfo[container.Image] = container.Resources.Requests.Cpu()
				} else {
					containerCpuRequestInfo[container.Image].Add(*container.Resources.Requests.Cpu())
				}

				if containerMemoryRequestInfo[container.Image] == nil {
					containerMemoryRequestInfo[container.Image] = container.Resources.Requests.Memory()
				} else {
					containerMemoryRequestInfo[container.Image].Add(*container.Resources.Requests.Memory())
				}

				// Count total amount of resources used limit/requests
				totalCpuLimits.Add(*container.Resources.Limits.Cpu())
				totalMemoryLimits.Add(*container.Resources.Limits.Memory())

				totalCpuRequests.Add(*container.Resources.Requests.Cpu())
				totalMemoryRequests.Add(*container.Resources.Requests.Memory())

				// Grab detailed info about resource usages
				detailedInfo[ns.Name][pod.Name][container.Image] = container.Resources
				totalCount += 1
			}
		}
	}

	cpuLimitRatio := float64(esCpuLimits.Value()) / float64(totalCpuLimits.Value())
	memoryLimitRatio := float64(esMemoryLimits.Value()) / float64(totalMemoryLimits.Value())

	cpuReqRatio := float64(esCpuRequests.Value()) / float64(totalCpuRequests.Value())
	memoryReqRatio := float64(esMemoryRequests.Value()) / float64(totalMemoryRequests.Value())

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
	memoryReqRatio float64, containerCpuLimitInfo map[string]*resource.Quantity,
	containerMemoryLimitInfo map[string]*resource.Quantity, containerMemoryRequestInfo map[string]*resource.Quantity,
	containerCpuRequestInfo map[string]*resource.Quantity) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"#", "Name", "Memory Limits", "CPU Limits", "Memory Requests", "CPU requests"})
	t.AppendRows([]table.Row{
		{1, "Total", totalMemoryLimits.String(), totalCpuLimits.String(),
			totalMemoryRequests.String(), totalCpuLimits.String()},
		{2, "Target", esMemoryLimits.String(), esCpuLimits.String(),
			esMemoryRequests.String(), esCpuRequests.String()},
		{3, "Ratio", fmt.Sprintf("%.2f", cpuLimitRatio), fmt.Sprintf("%.2f", memoryLimitRatio),
			fmt.Sprintf("%.2f", cpuReqRatio), fmt.Sprintf("%.2f", memoryReqRatio)},
	})
	i := 4
	for imageName := range containerCpuLimitInfo {
		t.AppendRows([]table.Row{
			{i, imageName, containerMemoryLimitInfo[imageName].String(), containerCpuLimitInfo[imageName].String(),
				containerMemoryRequestInfo[imageName].String(), containerCpuRequestInfo[imageName].String()},
		})
		i++
	}
	t.SetOutputMirror(out)
	t.RenderCSV()
}
