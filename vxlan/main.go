package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/TrilliumIT/iputil"
	cni "github.com/phdata/go-libcni"
	"github.com/phdata/vxlan-cni"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var exitOutput []byte
	exitCode := 0
	lf, err := os.OpenFile("/var/log/vxlan.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		exitCode, exitOutput = cni.PrepareExit(err, 99, "failed to open log file")
		return
	}
	defer lf.Close()
	log.SetOutput(lf)
	log.SetLevel(log.DebugLevel)

	defer func() {
		r := recover()
		if r != nil {
			err, ok := r.(error)
			if !ok {
				err = fmt.Errorf("panic: %v", r)
			}
			exitCode, exitOutput = cni.PrepareExit(err, 99, "panic during execution")
		}
		log.WithField("stdout", string(exitOutput)).Debug("ouptut")
		exit(exitCode, exitOutput)
	}()

	log.WithField("command", os.Getenv("CNI_COMMAND")).Debug()
	varNames := []string{"CNI_COMMAND", "CNI_CONTAINERID", "CNI_NETNS", "CNI_IFNAME", "CNI_ARGS", "CNI_PATH"}
	varMap := log.Fields{}
	for _, vn := range varNames {
		varMap[vn] = os.Getenv(vn)
	}
	log.WithFields(varMap).Debug("vars")

	//Read CNI standard environment variables
	vars := cni.NewVars()

	if vars.Command == "VERSION" {
		//report supported cni versions
		exitOutput = []byte(fmt.Sprintf("{\"cniVersion\": \"%v\", \"supportedVersions\": [\"%v\"]}", cni.CNIVersion, cni.CNIVersion))
		return
	}

	//Read and parse STDIN
	log.Debug("parsing stdin json")
	conf, err := parseStdin()
	if err != nil {
		exitCode, exitOutput = cni.PrepareExit(err, 6, "failed to parse STDIN")
		return
	}

	if conf.Args == nil {
		conf.Args = &cni.Args{}
	}

	if conf.Args.Annotations == nil {
		conf.Args.Annotations = make(map[string]string)
	}

	namespace, nsok := vars.GetArg("K8S_POD_NAMESPACE")
	podname, pnok := vars.GetArg("K8S_POD_NAME")

	//if "read from k8s" flag
	if conf.K8sReadAnnotations && nsok && pnok {
		annotations := getK8sAnnotations(conf.K8sConfigPath, namespace, podname)

		for k, v := range annotations {
			if _, ok := conf.Args.Annotations[k]; !ok {
				conf.Args.Annotations[k] = v
			}
		}
	}

	network, ok := conf.Args.Annotations[vxlan.NetworkAnnotation]
	if !ok {
		//if network is not specified in annotations
		if conf.K8sNetworkFromNamespace {
			network = namespace
		}
		if network == "" {
			network = conf.DefaultNetwork
		}
	}

	if network == "" {
		exitCode, exitOutput = cni.PrepareExit(nil, 7, "no network specified")
		return
	}

	var vxlp *vxlan.Vxlan
	for _, v := range conf.Vxlans {
		if v.Name == network {
			vxlp = v
			break
		}
	}

	if vxlp == nil {
		exitCode, exitOutput = cni.PrepareExit(nil, 7, "no matching network configured")
		return
	}

	lock, err := vxlan.NewLock(network)
	if err != nil {
		exitCode, exitOutput = cni.PrepareExit(err, 11, "failed to create lock file")
		return
	}

	lock.Lock()
	defer lock.Close()

	ipamBin := vars.Path + string(os.PathSeparator) + conf.Ipam.Type

	switch vars.Command {
	case "ADD":
		hostAddr, _ := netlink.ParseIPNet(vxlp.Cidr)
		address := iputil.NetworkID(hostAddr).String()

		reqAddress, ok := conf.Args.Annotations[vxlan.AddressAnnotation]
		if ok {
			ip := net.ParseIP(reqAddress)
			if ip != nil {
				if hostAddr.Contains(ip) {
					hostAddr.IP = ip
					address = hostAddr.String()
				}
			}
		}

		//get/create host interface
		hi, err := vxlan.GetOrCreateHostInterface(vxlp)
		if err != nil {
			exitCode, exitOutput = cni.PrepareExit(err, 11, "failed to get or create host interface")
			return
		}

		result, err := ipamAdd(ipamBin, address, vxlp.ExcludeFirst, vxlp.ExcludeLast)
		if err != nil {
			exitCode, exitOutput = cni.PrepareExit(err, 11, "failure to get address from IPAM")
			return
		}

		if len(result.IPs) < 1 || result.IPs[0].Address == "" {
			exitCode, exitOutput = cni.PrepareExit(nil, 11, "no IP was found in ipam result")
			return
		}

		rAddress := result.IPs[0].Address
		log.WithField("Address", rAddress).Debugf("ipam returned address")

		//add cmvl to host interface
		addr, _ := netlink.ParseIPNet(rAddress)
		li, err := hi.AddContainerLink(vars.NetworkNamespace, vars.ContainerInterface, addr)
		if err != nil {
			exitCode, exitOutput = cni.PrepareExit(err, 11, "failed to add container link to the macvlan bridge")
			err2 := ipamDel(ipamBin, rAddress)
			if err2 != nil {
				log.WithError(err2).Errorf("failure while running ipam delete")
			}
			return
		}

		result.Interfaces = append(result.Interfaces, &cni.Interface{
			Name:    vars.ContainerInterface,
			Sandbox: vars.NetworkNamespace,
		})

		result.IPs[0].Gateway = hi.GetGateway().IP.String()
		result.IPs[0].Interface = &li

		result.Routes = append(result.Routes, &cni.Route{
			Destination: "0.0.0.0/0",
			Gateway:     hi.GetGateway().IP.String(),
		})

		exitOutput = result.Marshal()
		return
	case "DEL":
		log.Debugf("getting host interface")
		//get/create host interface
		hi, err := vxlan.GetOrCreateHostInterface(vxlp)
		if err != nil {
			log.WithError(err).Errorf("failed to get or create host interface during DEL")
		}

		if hi != nil {
			log.Debugf("deleting container link")
			//delete cmvl
			err = hi.DeleteContainerLink(vars.NetworkNamespace, vars.ContainerInterface)
			if err != nil {
				log.WithError(err).Errorf("failed to delete container link")
			}
		}

		if conf.PreviousResult != nil && len(conf.PreviousResult.IPs) > 0 && conf.PreviousResult.IPs[0].Address != "" {
			err = ipamDel(ipamBin, conf.PreviousResult.IPs[0].Address)
			if err != nil {
				log.WithError(err).Errorf("failure while running ipam delete")
			}
		}

		//success
		return
		//TODO:
		//if last cmvl
		//delete host interface
	case "CHECK":
		return
		//if all "ADD" steps are correct
		//exit 0
		//else
		//exit error
	default:
		exitCode, exitOutput = cni.PrepareExit(fmt.Errorf("CNI_COMMAND was not set, or set to an invalid value"), 4, "invalid CNI_COMMAND")
		return
	}
}

func parseStdin() (*vxlan.Config, error) {
	//populate cni config from standard input
	scanner := bufio.NewScanner(os.Stdin)
	var confBytes []byte
	for scanner.Scan() {
		confBytes = append(confBytes, scanner.Bytes()...)
	}
	if len(confBytes) == 0 {
		return nil, fmt.Errorf("no bytes sent on stdin")
	}

	log.Debug(string(confBytes))

	return vxlan.NewConfig(confBytes)
}

func exit(code int, output []byte) {
	os.Stdout.Write(output)
	os.Exit(code)
}

func ipamAdd(bin, cidr string, xf, xl int) (*cni.Result, error) {
	log.Debugf("executing IPAM ADD")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(vxlan.DefaultIPAMTimeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), fmt.Sprintf("CNI_ARGS=CIDR=%v;EXCLUDE_FIRST=%v;EXCLUDE_LAST=%v", cidr, xf, xl))

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	if ctx.Err() == context.DeadlineExceeded {
		return nil, ctx.Err()
	}

	result := &cni.Result{}
	err = json.Unmarshal(out, result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func ipamDel(bin, cidr string) error {
	log.Debugf("executing IPAM DEL")
	//remove /32 route
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(vxlan.DefaultIPAMTimeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), fmt.Sprintf("CNI_ARGS=CIDR=%v", cidr))

	err := cmd.Run()
	if err != nil {
		log.WithError(err).Errorf("error while executing IPAM plugin during DEL")
		return err
	}

	if ctx.Err() == context.DeadlineExceeded {
		log.WithError(ctx.Err()).Errorf("timeout while executing IPAM plugin during DEL")
		return ctx.Err()
	}

	return nil
}

func getK8sAnnotations(kubeconfig, namespace, podname string) map[string]string {
	log.WithFields(log.Fields{"namespace": namespace, "podname": podname}).Debugf("getting annotations")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.WithError(err).Error("failed to get kubernetes config")
		return map[string]string{}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.WithError(err).Error("failed to get kubernetes client")
		return map[string]string{}
	}

	pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podname, metav1.GetOptions{})
	if err != nil {
		log.WithError(err).Error("failed to get pod")
		return map[string]string{}
	}

	log.WithField("annotations", pod.Annotations).Debug("retrieved annotations")
	return pod.Annotations
}
